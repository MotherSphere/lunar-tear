package questflow

import (
	"fmt"
	"log"

	"lunar-tear/server/internal/masterdata"
	"lunar-tear/server/internal/model"
	"lunar-tear/server/internal/store"
)

func (h *QuestHandler) initQuestState(user *store.UserState, questId int32) {
	quest := user.Quests[questId]
	quest.QuestId = questId
	user.Quests[questId] = quest

	for _, missionId := range h.MissionIdsByQuestId[questId] {
		key := store.QuestMissionKey{QuestId: questId, QuestMissionId: missionId}
		mission := user.QuestMissions[key]
		mission.QuestId = questId
		mission.QuestMissionId = missionId
		user.QuestMissions[key] = mission
	}
}

func isMainQuestPlayable(quest masterdata.EntityMQuest) bool {
	return !quest.IsRunInTheBackground && quest.IsCountedAsQuest
}

func (h *QuestHandler) clearQuestMissions(user *store.UserState, questId int32, nowMillis int64) {
	for _, missionId := range h.MissionIdsByQuestId[questId] {
		key := store.QuestMissionKey{QuestId: questId, QuestMissionId: missionId}
		mission := user.QuestMissions[key]
		mission.IsClear = true
		mission.ProgressValue = 1
		mission.LatestClearDatetime = nowMillis
		user.QuestMissions[key] = mission
	}
}

func (h *QuestHandler) HandleQuestStart(user *store.UserState, questId int32, isBattleOnly, isMainFlow bool, userDeckNumber int32, nowMillis int64) {
	h.handleQuestStartInternal(user, questId, isBattleOnly, isMainFlow, userDeckNumber, false, nowMillis)
}

func (h *QuestHandler) HandleQuestStartReplay(user *store.UserState, questId int32, isBattleOnly bool, userDeckNumber int32, nowMillis int64) {
	h.handleQuestStartInternal(user, questId, isBattleOnly, false, userDeckNumber, true, nowMillis)
}

func (h *QuestHandler) handleQuestStartInternal(user *store.UserState, questId int32, isBattleOnly, isMainFlow bool, userDeckNumber int32, isReplayFlow bool, nowMillis int64) {
	quest, ok := h.QuestById[questId]
	if !ok {
		panic(fmt.Sprintf("unknown questId=%d for HandleQuestStart", questId))
	}

	h.initQuestState(user, questId)
	if quest.Stamina > 0 {
		store.ConsumeStamina(user, quest.Stamina, h.MaxStaminaByLevel[user.Status.Level]*1000, nowMillis)
	}

	questState := user.Quests[questId]
	questState.IsBattleOnly = isBattleOnly
	questState.UserDeckNumber = userDeckNumber

	isCleared := questState.QuestStateType == model.UserQuestStateTypeCleared
	isMenuPick := !isReplayFlow && !isMainFlow && (isCleared || h.QuestHasDifficulty(questId))

	switch {
	case isMenuPick:
		snapshotMainQuestIfNeeded(user)
		sceneId := h.menuPickSceneId(questId, isBattleOnly)
		user.MainQuest.ProgressQuestSceneId = sceneId
		user.MainQuest.ProgressHeadQuestSceneId = sceneId
		user.MainQuest.ProgressQuestFlowType = int32(model.QuestFlowTypeMainFlow)
		user.PortalCageStatus.IsCurrentProgress = false
		user.PortalCageStatus.LatestVersion = nowMillis
		user.SideStoryActiveProgress = store.SideStoryActiveProgress{LatestVersion: nowMillis}
		user.MainQuest.LatestVersion = nowMillis
		log.Printf("[HandleQuestStart] QuestMenuPick quest=%d isBattleOnly=%v scene=%d cleared=%v",
			questId, isBattleOnly, sceneId, isCleared)

	case isCleared && isReplayFlow:
		snapshotMainQuestIfNeeded(user)
		user.MainQuest.CurrentQuestFlowType = int32(model.QuestFlowTypeReplayFlow)
		user.MainQuest.ReplayFlowCurrentQuestSceneId = 0
		user.MainQuest.ReplayFlowHeadQuestSceneId = 0
		user.MainQuest.LatestVersion = nowMillis
		log.Printf("[HandleQuestStart] MapPlay quest=%d isBattleOnly=%v", questId, isBattleOnly)
	}

	if isCleared {
		questState.QuestStateType = model.UserQuestStateTypeActive
		questState.LatestStartDatetime = nowMillis
		user.Quests[questId] = questState
		return
	}

	if isMainQuestPlayable(quest) {
		user.MainQuest.CurrentQuestFlowType = int32(model.QuestFlowTypeMainFlow)
		questState.QuestStateType = model.UserQuestStateTypeActive
		questState.LatestStartDatetime = nowMillis
	} else {
		questState.QuestStateType = model.UserQuestStateTypeCleared
		questState.ClearCount = 1
		questState.DailyClearCount = 1
		questState.LastClearDatetime = nowMillis
		if sceneIds := h.SceneIdsByQuestId[questId]; len(sceneIds) > 0 {
			firstSceneId := sceneIds[0]
			prevSceneId := user.MainQuest.CurrentQuestSceneId
			h.advanceMainFlowScene(user, questId, firstSceneId)
			user.MainQuest.CurrentQuestFlowType = int32(model.QuestFlowTypeMainFlow)
			log.Printf("[HandleQuestStart] background quest %d auto-cleared, scene %d -> %d", questId, prevSceneId, firstSceneId)
		}
	}
	user.Quests[questId] = questState
}

func snapshotMainQuestIfNeeded(user *store.UserState) {
	if user.MainQuest.SavedContext.Active {
		return
	}
	user.MainQuest.SavedContext = store.SavedQuestContext{
		Active:                  true,
		CurrentQuestSceneId:     user.MainQuest.CurrentQuestSceneId,
		HeadQuestSceneId:        user.MainQuest.HeadQuestSceneId,
		CurrentMainQuestRouteId: user.MainQuest.CurrentMainQuestRouteId,
		MainQuestSeasonId:       user.MainQuest.MainQuestSeasonId,
		IsReachedLastQuestScene: user.MainQuest.IsReachedLastQuestScene,
		PortalCageInProgress:    user.PortalCageStatus.IsCurrentProgress,
	}
}

func (h *QuestHandler) menuPickSceneId(questId int32, isBattleOnly bool) int32 {
	if isBattleOnly {
		if v, ok := h.BattleOnlyTargetSceneIdFor(questId); ok {
			return v
		}
	}
	if scenes := h.SceneIdsByQuestId[questId]; len(scenes) > 0 {
		return scenes[0]
	}
	return 0
}

func (h *QuestHandler) applyQuestVictory(user *store.UserState, questId int32, outcome *FinishOutcome, nowMillis int64) {
	questState := user.Quests[questId]
	if !questState.IsRewardGranted {
		h.applyQuestRewards(user, questId, nowMillis)
		outcome.ChangedWeaponStoryIds = append(outcome.ChangedWeaponStoryIds,
			h.grantWeaponStoryUnlocksForQuestScene(user, questId, model.QuestResultTypeHalfResult, nowMillis)...)
		outcome.ChangedWeaponStoryIds = append(outcome.ChangedWeaponStoryIds,
			h.grantWeaponStoryUnlocksForQuestScene(user, questId, model.QuestResultTypeFullResult, nowMillis)...)
		questState.IsRewardGranted = true
	}
	for _, drop := range outcome.DropRewards {
		h.applyRewardPossession(user, drop.PossessionType, drop.PossessionId, drop.Count, nowMillis)
	}
	for _, reward := range outcome.ReplayFlowFirstClearRewards {
		h.applyRewardPossession(user, reward.PossessionType, reward.PossessionId, reward.Count, nowMillis)
	}
	questState.QuestStateType = model.UserQuestStateTypeCleared
	questState.ClearCount++
	questState.DailyClearCount++
	questState.LastClearDatetime = nowMillis
	questState.IsBattleOnly = false
	user.Quests[questId] = questState
}

func (h *QuestHandler) finalizeChainPreviousQuest(user *store.UserState, questId int32, nowMillis int64) {
	if _, ok := h.QuestById[questId]; !ok {
		return
	}
	h.initQuestState(user, questId)
	questState := user.Quests[questId]
	if questState.QuestStateType == model.UserQuestStateTypeCleared {
		return
	}
	if !questState.IsRewardGranted {
		h.applyQuestRewards(user, questId, nowMillis)
		questState.IsRewardGranted = true
	}
	questState.QuestStateType = model.UserQuestStateTypeCleared
	questState.ClearCount++
	questState.DailyClearCount++
	questState.LastClearDatetime = nowMillis
	questState.IsBattleOnly = false
	user.Quests[questId] = questState
	h.clearQuestMissions(user, questId, nowMillis)
	log.Printf("[HandleMainQuestSceneProgress] finalized chain-previous quest %d (cleared)", questId)
}

func (h *QuestHandler) HandleQuestFinish(user *store.UserState, questId int32, isRetired, isAnnihilated bool, nowMillis int64) FinishOutcome {
	quest, ok := h.QuestById[questId]
	if !ok {
		panic(fmt.Sprintf("unknown questId=%d for HandleQuestFinish", questId))
	}

	h.initQuestState(user, questId)

	outcome := h.evaluateFinishOutcome(user, questId)
	wasReplay := user.MainQuest.CurrentQuestFlowType == int32(model.QuestFlowTypeReplayFlow)
	wasMenuReplay := user.MainQuest.SavedContext.Active

	if !isRetired {
		h.applyQuestVictory(user, questId, &outcome, nowMillis)

		if isMainQuestPlayable(quest) && !wasReplay && !wasMenuReplay {
			lastSceneId := h.getLastMainFlowSceneId(questId)
			h.advanceMainFlowScene(user, questId, lastSceneId)
		}
	}

	if isRetired && !isAnnihilated && quest.Stamina > 1 {
		refund := quest.Stamina - 1
		maxMillis := h.MaxStaminaByLevel[user.Status.Level] * 1000
		store.RecoverStamina(user, refund*1000, maxMillis, nowMillis)
	}

	// On retire of a previously-cleared quest (cage Menu Pick replay or
	// Map Play replay), HandleQuestStart marked QuestStateType=Active for
	// the run. With applyQuestVictory skipped on retire, that Active sticks
	// and the cage UI shows the quest as locked. Restore Cleared.
	if isRetired {
		qs := user.Quests[questId]
		if qs.ClearCount > 0 && qs.QuestStateType == model.UserQuestStateTypeActive {
			qs.QuestStateType = model.UserQuestStateTypeCleared
			user.Quests[questId] = qs
		}
	}

	user.MainQuest.ProgressQuestSceneId = 0
	user.MainQuest.ProgressHeadQuestSceneId = 0
	user.MainQuest.ProgressQuestFlowType = 0
	user.MainQuest.CurrentQuestFlowType = int32(model.QuestFlowTypeMainFlow)

	if wasMenuReplay {
		ctx := user.MainQuest.SavedContext
		user.MainQuest.CurrentQuestSceneId = ctx.CurrentQuestSceneId
		user.MainQuest.HeadQuestSceneId = ctx.HeadQuestSceneId
		user.MainQuest.CurrentMainQuestRouteId = ctx.CurrentMainQuestRouteId
		user.MainQuest.MainQuestSeasonId = ctx.MainQuestSeasonId
		user.MainQuest.IsReachedLastQuestScene = ctx.IsReachedLastQuestScene
		user.PortalCageStatus.IsCurrentProgress = ctx.PortalCageInProgress
		user.PortalCageStatus.LatestVersion = nowMillis
		user.MainQuest.SavedContext = store.SavedQuestContext{}
		user.MainQuest.LatestVersion = nowMillis
		log.Printf("[HandleQuestFinish] restored snapshot for quest %d (route=%d season=%d scene=%d head=%d cage=%v)",
			questId, ctx.CurrentMainQuestRouteId, ctx.MainQuestSeasonId,
			ctx.CurrentQuestSceneId, ctx.HeadQuestSceneId, ctx.PortalCageInProgress)
	}

	if wasReplay {
		user.MainQuest.ReplayFlowCurrentQuestSceneId = 0
		user.MainQuest.ReplayFlowHeadQuestSceneId = 0
		log.Printf("[HandleQuestFinish] replay flow ended for quest %d", questId)
	}

	h.clearQuestMissions(user, questId, nowMillis)

	return outcome
}

func (h *QuestHandler) HandleQuestSkip(user *store.UserState, questId, skipCount int32, nowMillis int64) FinishOutcome {
	questDef, ok := h.QuestById[questId]
	if !ok {
		panic(fmt.Sprintf("unknown questId=%d for HandleQuestSkip", questId))
	}

	maxMillis := h.MaxStaminaByLevel[user.Status.Level] * 1000
	store.ConsumeStamina(user, skipCount, maxMillis, nowMillis)

	skipTicketId := h.Config.ConsumableItemIdForQuestSkipTicket
	user.ConsumableItems[skipTicketId] -= skipCount
	if user.ConsumableItems[skipTicketId] < 0 {
		user.ConsumableItems[skipTicketId] = 0
	}

	var allDrops []RewardGrant
	for range skipCount {
		drops := h.computeDropRewards(questDef)
		for _, drop := range drops {
			h.applyRewardPossession(user, drop.PossessionType, drop.PossessionId, drop.Count, nowMillis)
		}
		allDrops = append(allDrops, drops...)

		if questDef.Gold != 0 {
			user.ConsumableItems[h.Config.ConsumableItemIdForGold] += questDef.Gold
		}
		h.applyExpRewards(user, questId, nowMillis)
	}

	questState := user.Quests[questId]
	questState.ClearCount += skipCount
	questState.DailyClearCount += skipCount
	questState.LastClearDatetime = nowMillis
	user.Quests[questId] = questState

	log.Printf("[HandleQuestSkip] questId=%d skipCount=%d drops=%d gold=%d", questId, skipCount, len(allDrops), questDef.Gold*skipCount)
	return FinishOutcome{DropRewards: allDrops}
}

func (h *QuestHandler) HandleQuestRestart(user *store.UserState, questId int32, nowMillis int64) {
	questDef, ok := h.QuestById[questId]
	// Only seed CurrentQuestFlowType when it's not already set (initial
	// natural progression). Don't clobber an in-flight ReplayFlow (Map Play
	// resume).
	if ok && isMainQuestPlayable(questDef) && user.MainQuest.CurrentQuestFlowType == 0 {
		user.MainQuest.CurrentQuestFlowType = int32(model.QuestFlowTypeMainFlow)
	}

	quest := user.Quests[questId]
	quest.QuestId = questId
	quest.QuestStateType = model.UserQuestStateTypeActive
	quest.LatestStartDatetime = nowMillis
	user.Quests[questId] = quest

	for _, missionId := range h.MissionIdsByQuestId[questId] {
		key := store.QuestMissionKey{QuestId: questId, QuestMissionId: missionId}
		m := user.QuestMissions[key]
		m.QuestId = questId
		m.QuestMissionId = missionId
		m.IsClear = false
		m.ProgressValue = 0
		m.LatestClearDatetime = 0
		user.QuestMissions[key] = m
	}
}

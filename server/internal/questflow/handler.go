package questflow

import (
	"lunar-tear/server/internal/masterdata"
	"lunar-tear/server/internal/model"
	"lunar-tear/server/internal/store"
)

type RewardGrant struct {
	PossessionType model.PossessionType
	PossessionId   int32
	Count          int32
}

type FinishOutcome struct {
	DropRewards                  []RewardGrant
	FirstClearRewards            []RewardGrant
	ReplayFlowFirstClearRewards  []RewardGrant
	MissionClearRewards          []RewardGrant
	MissionClearCompleteRewards  []RewardGrant
	BigWinClearedQuestMissionIds []int32
	IsBigWin                     bool
	ChangedWeaponStoryIds        []int32
}

type QuestHandler struct {
	*masterdata.QuestCatalog
	Config                         *masterdata.GameConfig
	Granter                        *store.PossessionGranter
	SideStoryChapterByEventQuestId map[int32]int32
}

func NewQuestHandler(catalog *masterdata.QuestCatalog, config *masterdata.GameConfig, sideStory *masterdata.SideStoryCatalog) *QuestHandler {
	granter := BuildGranter(catalog)
	var sideStoryChapters map[int32]int32
	if sideStory != nil {
		sideStoryChapters = sideStory.ChapterByEventQuestId
	}
	return &QuestHandler{
		QuestCatalog:                   catalog,
		Config:                         config,
		Granter:                        granter,
		SideStoryChapterByEventQuestId: sideStoryChapters,
	}
}

func BuildGranter(catalog *masterdata.QuestCatalog) *store.PossessionGranter {
	costumeById := make(map[int32]store.CostumeRef, len(catalog.CostumeById))
	for id, cm := range catalog.CostumeById {
		costumeById[id] = store.CostumeRef{CharacterId: cm.CharacterId}
	}
	weaponById := make(map[int32]store.WeaponRef, len(catalog.WeaponById))
	for id, wm := range catalog.WeaponById {
		weaponById[id] = store.WeaponRef{
			WeaponSkillGroupId:                 wm.WeaponSkillGroupId,
			WeaponAbilityGroupId:               wm.WeaponAbilityGroupId,
			WeaponStoryReleaseConditionGroupId: wm.WeaponStoryReleaseConditionGroupId,
		}
	}
	releaseConditions := make(map[int32][]store.WeaponStoryReleaseCond, len(catalog.ReleaseConditionsByGroupId))
	for groupId, rows := range catalog.ReleaseConditionsByGroupId {
		conds := make([]store.WeaponStoryReleaseCond, len(rows))
		for i, r := range rows {
			conds[i] = store.WeaponStoryReleaseCond{
				StoryIndex:                      r.StoryIndex,
				WeaponStoryReleaseConditionType: model.WeaponStoryReleaseConditionType(r.WeaponStoryReleaseConditionType),
				ConditionValue:                  r.ConditionValue,
			}
		}
		releaseConditions[groupId] = conds
	}
	partsById := make(map[int32]store.PartsRef, len(catalog.PartsById))
	for id, p := range catalog.PartsById {
		partsById[id] = store.PartsRef{
			PartsGroupId:                  p.PartsGroupId,
			PartsStatusMainLotteryGroupId: p.PartsStatusMainLotteryGroupId,
		}
	}
	return &store.PossessionGranter{
		CostumeById:                          costumeById,
		WeaponById:                           weaponById,
		WeaponSkillSlots:                     catalog.WeaponSkillSlots,
		WeaponAbilitySlots:                   catalog.WeaponAbilitySlots,
		ReleaseConditions:                    releaseConditions,
		PartsById:                            partsById,
		DefaultPartsStatusMainByLotteryGroup: catalog.DefaultPartsStatusMainByLotteryGroup,
	}
}

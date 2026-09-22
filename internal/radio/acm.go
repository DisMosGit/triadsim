package radio

import "github.com/DisMosGit/triadsim/internal/model"

// selectProfile returns the modulation profile a received level selects.
//
// A disabled or fixed ACM keeps the configured current profile. An adaptive one
// picks the highest profile of the configured window whose receiver threshold
// the level still meets — the highest capacity the link can carry — and
// degrades to the most robust profile of the window when the level meets none.
//
// The boolean is false when no profile applies at all (an empty table, or a
// current/min profile the table does not hold); the caller then leaves the
// link's fade margin and capacity untouched.
func selectProfile(acm model.ACM, profiles []model.ModProfile, rssi float64) (model.ModProfile, bool) {
	if len(profiles) == 0 {
		return model.ModProfile{}, false
	}
	if !acm.Enabled || acm.Mode == model.ACMModeFixed {
		if profile, ok := profileByID(profiles, acm.CurrentProfile); ok {
			return profile, true
		}
		return profileByID(profiles, acm.MinProfile)
	}

	best, found := model.ModProfile{}, false
	for _, profile := range profiles {
		if profile.ID < acm.MinProfile || profile.ID > acm.MaxProfile {
			continue
		}
		if profile.RSLThreshold > rssi {
			continue
		}
		if !found || profile.ID > best.ID {
			best, found = profile, true
		}
	}
	if found {
		return best, true
	}
	return profileByID(profiles, acm.MinProfile)
}

// profileByID returns the profile with the given identifier.
func profileByID(profiles []model.ModProfile, id uint8) (model.ModProfile, bool) {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return model.ModProfile{}, false
}

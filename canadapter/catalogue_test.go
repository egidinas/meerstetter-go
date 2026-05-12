package canadapter

import (
	"strings"
	"testing"
)

func TestFirstClassProfilesIncludePiXtendAndKvaser(t *testing.T) {
	profiles := []string{ProfilePiXtendV2L, ProfileKvaserUSB, ProfileKvaserDINRail}
	for _, id := range profiles {
		profile, ok := LookupProfile(id)
		if !ok {
			t.Fatalf("profile %s not found", id)
		}
		if !profile.FirstClass {
			t.Fatalf("profile %s should be first-class", id)
		}
		if len(profile.DefaultBitrates) == 0 {
			t.Fatalf("profile %s has no bitrate defaults", id)
		}
	}
}

func TestPiXtendChecklistCapturesKnownFailureGates(t *testing.T) {
	profile := MustProfile(ProfilePiXtendV2L)
	checklist := RenderChecklist(profile)
	for _, want := range []string{
		"tier: first-class",
		"dtparam=spi=on",
		"pixtendv2l",
		"analog-output/CAN jumper",
		"timeout 2s pixtendsrv2",
	} {
		if !strings.Contains(checklist, want) {
			t.Fatalf("PiXtend checklist missing %q:\n%s", want, checklist)
		}
	}
}

func TestMatchSocketCANDriverMapsPopularAdapters(t *testing.T) {
	cases := map[string]string{
		"mcp251x":    ProfilePiXtendV2L,
		"gs_usb":     ProfileCandleLight,
		"peak_usb":   ProfilePCANUSB,
		"kvaser_usb": ProfileKvaserUSB,
	}
	for driver, wantID := range cases {
		matches := MatchSocketCANDriver(driver)
		if len(matches) == 0 {
			t.Fatalf("driver %s returned no matches", driver)
		}
		if matches[0].ID != wantID {
			t.Fatalf("driver %s matched %s, want %s", driver, matches[0].ID, wantID)
		}
	}
}

func TestEasyUnprovenProfilesCarryDriverReferences(t *testing.T) {
	for _, id := range []string{
		ProfileCandleLight,
		ProfilePCANUSB,
		ProfileEMSUSB,
		ProfileESDUSB,
		ProfileMicrochipCAN,
		ProfileCAN327,
		ProfileSLCANUSB,
	} {
		profile := MustProfile(id)
		if profile.FirstClass {
			t.Fatalf("profile %s should not be first-class", id)
		}
		if profile.Maturity != MaturityEasyUnproven {
			t.Fatalf("profile %s maturity = %s, want %s", id, profile.Maturity, MaturityEasyUnproven)
		}
		if len(profile.DriverRefs) == 0 {
			t.Fatalf("profile %s has no driver references", id)
		}
	}
}

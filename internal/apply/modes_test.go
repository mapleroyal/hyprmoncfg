package apply

import (
	"strings"
	"testing"

	"github.com/crmne/hyprmoncfg/internal/hypr"
	"github.com/crmne/hyprmoncfg/internal/profile"
)

func TestValidateAdvertisedModes(t *testing.T) {
	for _, tc := range []struct {
		name           string
		mode           string
		available      []string
		currentWidth   int
		disabled       bool
		outputDisabled bool
		wantError      bool
	}{
		{"partial dock list", "1920x1080@60Hz", []string{"1600x900@60Hz"}, 1600, false, false, true},
		{"advertised pair", "1920x1080@60Hz", []string{"1920x1080@60.00Hz"}, 1600, false, false, false},
		{"refresh rounding", "1920x1080@60Hz", []string{"1920x1080@59.94Hz"}, 1600, false, false, false},
		{"wrong refresh", "1920x1080@120Hz", []string{"1920x1080@60Hz"}, 1600, false, false, true},
		{"resolution only", "1920x1080", []string{"1920x1080@144Hz"}, 1600, false, false, false},
		{"already driven", "1920x1080@60Hz", []string{"1600x900@60Hz"}, 1920, false, false, false},
		{"disabled stale mode", "1920x1080@60Hz", []string{"1600x900@60Hz"}, 1920, true, false, true},
		{"intentional disable", "1920x1080@60Hz", []string{"1600x900@60Hz"}, 1600, false, true, false},
		{"missing capability", "1920x1080@60Hz", nil, 1600, false, false, false},
		{"preferred", "preferred", []string{"1600x900@60Hz"}, 1600, false, false, false},
		{"custom modeline", "modeline 148.5 1920 2008 2052 2200 1080 1084 1089 1125", []string{"1600x900@60Hz"}, 1600, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := hypr.Monitor{Name: "DP-2", Make: "Dell", Model: "Panel", Serial: "B", Width: tc.currentWidth, Height: 1080, RefreshRate: 60, Disabled: tc.disabled, AvailableModes: tc.available}
			p := profile.FromMonitors("Work", []hypr.Monitor{m})
			p.Outputs[0].Mode = tc.mode
			p.Outputs[0].Enabled = !tc.outputDisabled
			err := ValidateAdvertisedModes(p, []hypr.Monitor{m})
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, wantError %v", err, tc.wantError)
			}
		})
	}
}

func TestValidateAdvertisedModesUsesHardwareIdentityAndReportsAllFailures(t *testing.T) {
	monitors := []hypr.Monitor{
		{Name: "DP-2", Make: "Dell", Model: "Panel", Serial: "A", Width: 1600, Height: 900, RefreshRate: 60, AvailableModes: []string{"1600x900@60Hz"}},
		{Name: "DP-1", Make: "Dell", Model: "Panel", Serial: "B", Width: 1600, Height: 900, RefreshRate: 60, AvailableModes: []string{"1600x900@60Hz"}},
	}
	p := profile.FromMonitors("Work", monitors)
	for i := range p.Outputs {
		p.Outputs[i].Mode = "1920x1080@60Hz"
		// Connector names in a saved profile may be reversed after reconnect.
		if p.Outputs[i].Name == "DP-1" {
			p.Outputs[i].Name = "DP-2"
		} else {
			p.Outputs[i].Name = "DP-1"
		}
	}
	err := ValidateAdvertisedModes(p, monitors)
	if err == nil || !strings.Contains(err.Error(), "DP-1") || !strings.Contains(err.Error(), "DP-2") {
		t.Fatalf("expected both physical displays in error: %v", err)
	}
	if err := ValidateAdvertisedModes(p, nil); err != nil {
		t.Fatalf("disconnected outputs should not block: %v", err)
	}
}

func TestValidateAdvertisedModesChecksEnabledMirrors(t *testing.T) {
	monitors := []hypr.Monitor{
		{Name: "DP-1", Make: "Dell", Model: "Panel", Serial: "A", Width: 1920, Height: 1080, RefreshRate: 60},
		{Name: "DP-2", Make: "Dell", Model: "Panel", Serial: "B", Width: 1600, Height: 900, RefreshRate: 60, AvailableModes: []string{"1600x900@60Hz"}},
	}
	p := profile.FromMonitors("Mirror", monitors)
	for i := range p.Outputs {
		if p.Outputs[i].Serial == "B" {
			p.Outputs[i].MirrorOf = monitors[0].HardwareKey()
			p.Outputs[i].Mode = "1920x1080@60Hz"
		}
	}
	if err := ValidateAdvertisedModes(p, monitors); err == nil {
		t.Fatal("enabled mirror with unsupported fixed mode must defer")
	}
}

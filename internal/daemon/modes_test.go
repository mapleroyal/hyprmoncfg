package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/crmne/hyprmoncfg/internal/hypr"
	"github.com/crmne/hyprmoncfg/internal/ipc"
	"github.com/crmne/hyprmoncfg/internal/profile"
)

func TestInteractiveUnadvertisedModeCanBeConfirmedAfterAutomaticDeferral(t *testing.T) {
	before := []hypr.Monitor{{
		Name: "DP-1", Make: "Dell", Model: "Panel", Serial: "A",
		Width: 1600, Height: 900, RefreshRate: 60, Scale: 1, DPMSStatus: true,
		AvailableModes: []string{"1600x900@60Hz"},
	}}
	after := append([]hypr.Monitor(nil), before...)
	after[0].Width, after[0].Height = 1920, 1080
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	env := newApplyBestTestEnvWithMonitors(t, string(beforeJSON), string(afterJSON))
	target := profile.FromMonitors("Work", after)
	if err := env.store.Save(target); err != nil {
		t.Fatal(err)
	}
	svc := New(env.client, env.store, Config{
		MonitorsConf: env.monitorsConfPath, HyprConfig: env.hyprlandConfigPath,
	})
	original := readMonitorsConf(t, env)
	for range 3 {
		if err := svc.applyAutomatic(context.Background()); err == nil || !strings.Contains(err.Error(), "automatic profile \"Work\" deferred:") {
			t.Fatalf("expected automatic fixed-mode deferral, got %v", err)
		}
	}
	if reloadCount(env.logPath) != 0 || readMonitorsConf(t, env) != original {
		t.Fatal("automatic deferral changed the live or generated layout")
	}
	// An explicit preview must still let Hyprland try the numeric mode even
	// though it remains absent from AvailableModes after successful readback.
	transaction, err := svc.Preview("test-panel", ipc.PreviewParams{ProfileName: "Work", TimeoutSeconds: 10})
	if err != nil {
		t.Fatalf("interactive unadvertised-mode preview failed: %v", err)
	}
	if err := svc.Confirm("test-panel", ipc.TransactionParams{TransactionID: transaction.ID}); err != nil {
		t.Fatal(err)
	}
	document, err := svc.Status()
	if err != nil {
		t.Fatal(err)
	}
	if document.Daemon.Preview != nil || document.Daemon.ProfileOverride != "Work" {
		t.Fatalf("confirmation did not establish a manual override: %+v", document.Daemon)
	}
	live, err := svc.queryMonitors(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].Disabled || !live[0].DPMSStatus || live[0].Width != 1920 || live[0].Height != 1080 || len(live[0].AvailableModes) != 1 || live[0].AvailableModes[0] != "1600x900@60Hz" {
		t.Fatalf("expected usable unadvertised mode after confirmation: %+v", live)
	}
	confirmed := readMonitorsConf(t, env)
	for range 5 {
		if err := svc.applyAutomatic(context.Background()); err != nil {
			t.Fatalf("confirmed interactive mode was deferred again: %v", err)
		}
	}
	if got := reloadCount(env.logPath); got != 1 || readMonitorsConf(t, env) != confirmed {
		t.Fatalf("confirmed override was reapplied or rolled back: reloads=%d", got)
	}
}

func TestAutomaticModePreflightPreservesLayoutAndRecoversWithoutHotplug(t *testing.T) {
	monitors := []hypr.Monitor{
		{Name: "DP-1", Make: "Dell", Model: "Panel", Serial: "A", Width: 1920, Height: 1080, RefreshRate: 60, Scale: 1, DPMSStatus: true, AvailableModes: []string{"1920x1080@60Hz"}},
		{Name: "DP-2", Make: "Dell", Model: "Panel", Serial: "B", Width: 1600, Height: 900, RefreshRate: 60, X: 1920, Scale: 1, DPMSStatus: true, AvailableModes: []string{"1600x900@60Hz"}},
	}
	target := profile.FromMonitors("Work", monitors)
	for i := range target.Outputs {
		if target.Outputs[i].Serial == "A" {
			target.Outputs[i].Transform = 1
		} else {
			target.Outputs[i].Mode = "1920x1080@60Hz"
			target.Outputs[i].Width, target.Outputs[i].Height = 1920, 1080
			target.Outputs[i].X = 1080
		}
	}
	env := newRunTestEnv(t, monitors, func(s *Service) {
		s.cfg.ForcedProfile = "Work"
		s.cfg.RecoveryInterval = 10 * time.Millisecond
		s.cfg.RecoveryMaxInterval = 20 * time.Millisecond
		if err := s.store.Save(target); err != nil {
			t.Fatal(err)
		}
	})
	defer env.stop()
	before, err := os.ReadFile(env.monitorsConfPath)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return strings.Count(env.logs.all(), "automatic profile \"Work\" deferred:") >= 3 }, "read-only retries of partial dock modes")
	if got := reloadCount(env.logPath); got != 0 {
		t.Fatalf("unsupported mode reloaded %d times", got)
	}
	after, err := os.ReadFile(env.monitorsConfPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("unsupported mode changed generated layout")
	}
	// Capabilities settle without a topology or live-mode change. The next
	// read-only recovery pass must detect this even though the state hash is unchanged.
	monitors[1].AvailableModes = []string{"1600x900@60Hz", "1920x1080@60Hz"}
	ready := append([]hypr.Monitor(nil), monitors...)
	ready[0].Transform = 1
	ready[1].Width, ready[1].Height, ready[1].X = 1920, 1080, 1080
	if err := writeMonitorState(env.monitorStatePath+".next", ready); err != nil {
		t.Fatal(err)
	}
	if err := writeMonitorState(env.monitorStatePath, monitors); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return env.logs.contains("applied profile: Work") }, "successful retry after mode advertisement changes")
	time.Sleep(150 * time.Millisecond)
	if got := reloadCount(env.logPath); got != 1 {
		t.Fatalf("expected one successful layout apply, got %d\n%s", got, env.logs.all())
	}
	saved, err := env.svc.store.Load("Work")
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range saved.Outputs {
		if output.Width != 1920 || output.Height != 1080 {
			t.Fatalf("saved profile mode was changed: %+v", output)
		}
	}
}

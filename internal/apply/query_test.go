package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmne/hyprmoncfg/internal/hypr"
)

func TestQueryDistinguishesParentDeadlineFromReadTimeout(t *testing.T) {
	for _, parentExpires := range []bool{false, true} {
		t.Run(map[bool]string{false: "read timeout", true: "parent deadline"}[parentExpires], func(t *testing.T) {
			ctx := context.Background()
			engine := Engine{QueryTimeout: 20 * time.Millisecond}
			if parentExpires {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
			}
			_, err := query(ctx, engine, "monitors", func(readCtx context.Context) (int, error) {
				<-readCtx.Done()
				return 0, readCtx.Err()
			})
			if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrQueryTimeout) == parentExpires {
				t.Fatalf("wrong timeout classification: %v", err)
			}
		})
	}
}

func TestValidationParentDeadlinePreservesRefreshMismatch(t *testing.T) {
	engine, logPath, err := initTestEngine(t)
	if err != nil {
		t.Fatal(err)
	}
	engine.QueryTimeout = 2 * time.Second
	helper := filepath.Join(filepath.Dir(logPath), "hyprctl")
	source, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	// The first read reports a rejected mode. The next read outlives the
	// validation context, reproducing expiry during an otherwise bounded read.
	anchor := `if [ "$cmd1" = "-j" ] && [ "$cmd2" = "monitors" ] && [ "$cmd3" = "all" ]; then`
	blocked := strings.Replace(string(source), anchor, anchor+"\n  if [ -f \"$HYPRCTL_LOG.read\" ]; then exec sleep 20; fi\n  touch \"$HYPRCTL_LOG.read\"", 1)
	if err := os.WriteFile(helper, []byte(blocked), 0755); err != nil {
		t.Fatal(err)
	}
	p := newTestProfile()
	p.Outputs[0].Refresh = 60
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err = engine.waitForAppliedProfile(ctx, p, append([]hypr.Monitor(nil), monitors...))
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrQueryTimeout) || !strings.Contains(err.Error(), "refresh mismatch") {
		t.Fatalf("validation lost the rejected mode: %v", err)
	}
}

func TestLuaProbeReadTimeoutRestoresConfig(t *testing.T) {
	initial := []byte("-- previous generated layout\n")
	engine, target, logPath := initLuaProbeTestEngine(t, "ok", initial)
	engine.QueryTimeout = 40 * time.Millisecond
	helper := filepath.Join(filepath.Dir(logPath), "hyprctl")
	source, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	blocked := strings.Replace(string(source), `  printf '%s' "$HYPRMONCFG_TEST_EVAL_REPLY"`, "  exec sleep 20", 1)
	if err := os.WriteFile(helper, []byte(blocked), 0755); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = engine.Apply(context.Background(), newTestProfile(), monitors, ApplyModeInteractive)
	if !errors.Is(err, ErrQueryTimeout) || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("Lua verification was not bounded: elapsed=%s error=%v", time.Since(started), err)
	}
	if restored, err := os.ReadFile(target); err != nil || string(restored) != string(initial) {
		t.Fatalf("timed-out Lua read did not restore config: %s (%v)", restored, err)
	}
}

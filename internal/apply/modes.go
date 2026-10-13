package apply

import (
	"errors"
	"fmt"
	"math"

	"github.com/crmne/hyprmoncfg/internal/hypr"
	"github.com/crmne/hyprmoncfg/internal/profile"
)

// ValidateAdvertisedModes prevents an automatic apply/rollback loop while a
// dock advertises only a partial mode list. It is deliberately separate from
// Engine.Apply: an interactive preview may request a custom, unadvertised mode.
func ValidateAdvertisedModes(p profile.Profile, monitors []hypr.Monitor) error {
	resolver := profile.NewMonitorResolver(monitors)
	var problems []error
	for _, output := range p.Outputs {
		if !output.Enabled {
			continue
		}
		monitor, found := resolver.ResolveOutput(output)
		if !found || len(monitor.AvailableModes) == 0 {
			// Missing capabilities are not proof that the mode is unsupported.
			continue
		}
		width, height, refresh, fixed := hypr.ParseMode(output.NormalizedMode())
		if !fixed || width <= 0 || height <= 0 {
			// preferred, highres, highrr and custom modelines remain compositor-owned.
			continue
		}
		matches := func(mode string) bool {
			w, h, hz, ok := hypr.ParseMode(mode)
			return ok && w == width && h == height && (refresh <= 0 || math.Abs(hz-refresh) <= 0.1)
		}
		if !monitor.Disabled && matches(monitor.ModeString()) {
			continue
		}
		supported := false
		for _, mode := range monitor.AvailableModes {
			if matches(mode) {
				supported = true
				break
			}
		}
		if !supported {
			problems = append(problems, fmt.Errorf("%s does not advertise saved mode %s; leaving the current layout unchanged until the mode is available", monitor.Name, output.NormalizedMode()))
		}
	}
	return errors.Join(problems...)
}

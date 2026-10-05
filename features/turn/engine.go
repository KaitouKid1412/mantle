package turn

import (
	"context"

	"github.com/KaitouKid1412/mantle/internal/engine"
)

// engineAdapter is the engine version gate's checker: plan 02's conformance probe and
// pinning. CheckEngine probes only a version it hasn't seen pass, and with
// $MANTLE_CLAUDE_BIN set (development, fakeclaude) it checks the version alone.
type engineAdapter struct{}

func (engineAdapter) Check(ctx context.Context) (engineCheck, error) {
	c, err := engine.CheckEngine(ctx, engine.CheckOptions{})
	if err != nil {
		return engineCheck{}, err
	}
	out := engineCheck{
		OK: c.OK, Probed: c.Probed, Version: c.Version,
		LastGood: c.LastGood, LastGoodBinary: c.LastGoodBinary,
	}
	if c.TooOld {
		out.Failed = append(out.Failed, "older than "+engine.MinEngineVersion)
	}
	if c.Result != nil {
		out.Failed = append(out.Failed, c.Result.Failed()...)
	}
	return out, nil
}

// Pin records the pin where the engine manager resolves its binary.
func (engineAdapter) Pin(binary, version string) error {
	return engine.Pin(engine.DefaultEngineStatePath(), binary, version)
}

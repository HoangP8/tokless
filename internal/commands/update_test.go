package commands

import (
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestUpdateToolFlowSelectionAndInstallation(t *testing.T) {
	for _, id := range []string{"projectmem", "headroom"} {
		needsInstall, _, _ := updateToolFlow(id, util.VersionInfo{}, nil)
		if !needsInstall {
			t.Errorf("%s missing install despite unknown latest version", id)
		}

		called := false
		_, verified, err := updateToolFlow(id, util.VersionInfo{}, func() (bool, bool, error) {
			called = true
			return true, false, nil
		})
		if !called || err != nil || verified {
			t.Errorf("%s accepted installer success without package presence", id)
		}

		_, verified, err = updateToolFlow(id, util.VersionInfo{}, func() (bool, bool, error) {
			return true, true, nil
		})
		if err != nil || !verified {
			t.Errorf("%s rejected installer success with package presence", id)
		}
	}
}

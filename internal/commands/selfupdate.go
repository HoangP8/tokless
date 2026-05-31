package commands

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

const owner = "HoangP8"
const repo = "tokless"
const installSh = "https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.sh"
const installPs1 = "https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.ps1"

func RunSelfUpdate() int {
	util.L.Banner("tokless self-update", "Update the tokless CLI itself")
	local := util.ToklessVersion()
	util.L.Sub("local: " + local)
	latest := fetchLatestReleaseTag()
	if latest == "" {
		util.L.Err("Could not reach GitHub Releases. Try again later.")
		util.L.Warn("Manual update:")
		util.L.Raw("  " + util.C.Cyan("curl -fsSL "+installSh+" | bash"))
		return 1
	}
	util.L.Sub("latest: " + latest)
	if util.SemverGte(local, latest) {
		util.L.Ok("Already up to date.")
		return 0
	}
	util.L.Step("Updating " + local + " → " + latest + "…")
	if util.IsWin {
		util.L.Warn("On Windows, run:")
		util.L.Raw("  " + util.C.Cyan("irm "+installPs1+" | iex"))
		return 0
	}
	if util.Which("curl") != "" && util.Which("bash") != "" {
		r := util.Run("bash", []string{"-c", "curl -fsSL " + installSh + " | bash"}, util.RunOptions{})
		if r.Code == 0 {
			util.L.Ok("Updated to " + latest + ". Restart your shell if needed.")
			return 0
		}
		util.L.Err("Auto-update failed.")
	}
	util.L.Warn("Manual update:")
	util.L.Raw("  " + util.C.Cyan("curl -fsSL "+installSh+" | bash"))
	return 0
}

func fetchLatestReleaseTag() string {
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+owner+"/"+repo+"/releases/latest", nil)
	req.Header.Set("User-Agent", "tokless-self-update")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var j struct {
		TagName string `json:"tag_name"`
	}
	if json.NewDecoder(resp.Body).Decode(&j) != nil {
		return ""
	}
	return strings.TrimPrefix(j.TagName, "v")
}

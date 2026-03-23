package versioner

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const goDevDLAPI = "https://go.dev/dl/?mode=json"

type goDevRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

// GoDevLatestVersion returns the latest stable Go version (e.g. "go1.26.1").
func GoDevLatestVersion() (string, error) {
	releases, err := fetchGoDevReleases(goDevDLAPI)
	if err != nil {
		return "", err
	}
	for _, r := range releases {
		if r.Stable {
			return r.Version, nil
		}
	}
	return "", fmt.Errorf("no stable Go release found")
}

// GoDevListVersions returns all stable Go versions.
func GoDevListVersions() ([]string, error) {
	releases, err := fetchGoDevReleases(goDevDLAPI + "&include=all")
	if err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(releases))
	for _, r := range releases {
		if r.Stable {
			versions = append(versions, r.Version)
		}
	}
	return versions, nil
}

func fetchGoDevReleases(url string) ([]goDevRelease, error) {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("fetch go.dev releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("go.dev API returned %d", resp.StatusCode)
	}

	var releases []goDevRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode go.dev response: %w", err)
	}
	return releases, nil
}

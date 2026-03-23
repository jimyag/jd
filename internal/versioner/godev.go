package versioner

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const goDevDLAPI = "https://go.dev/dl/?mode=json"

// GoDev implements Versioner using the go.dev/dl API.
type GoDev struct{}

func (g *GoDev) Latest() (string, error) {
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

func (g *GoDev) List() ([]string, error) {
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

type goDevRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
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

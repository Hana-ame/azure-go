package psda1proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// ipInfo is the JSON response from ip-api.com.
type ipInfo struct {
	Status      string `json:"status"`      // "success" or "fail"
	Message     string `json:"message"`     // error message on failure
	IP          string `json:"query"`       // the IP that was queried
	Country     string `json:"country"`
	CountryCode string `json:"countryCode"` // "CN" for China
	City        string `json:"city"`
	Region      string `json:"region"`
	Proxy       bool   `json:"proxy"`
}

// CheckChinaIP queries ip-api.com using the given client and returns the
// outbound IP and whether it is located in China (countryCode == "CN").
//
// The client should be created by NewClient() so that the check itself
// also uses local IPv4 with no proxy – this verifies the egress path
// that the upload traffic will actually use.
func CheckChinaIP(client *http.Client) (ip string, isChina bool, err error) {
	url := "http://ip-api.com/json/?fields=status,message,country,countryCode,query,proxy"

	resp, err := client.Get(url)
	if err != nil {
		return "", false, fmt.Errorf("psda1proxy: ip check request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, fmt.Errorf("psda1proxy: ip check read body: %w", err)
	}

	var info ipInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return "", false, fmt.Errorf("psda1proxy: ip check parse response: %w", err)
	}
	if info.Status != "success" {
		return "", false, fmt.Errorf("psda1proxy: ip check not successful: %s", info.Message)
	}
	if info.CountryCode == "CN" {
		return info.IP, true, nil
	}
	return info.IP, false, nil
}

// --- Cached IP check (checked once, reused thereafter) ---

var (
	ipCheckOnce   sync.Once
	ipCheckIP     string
	ipCheckChina  bool
	ipCheckErr    error
)

// GetChinaIPCheck performs the IP check exactly once (process-wide) and
// returns the cached result. This avoids hammering ip-api.com on every
// upload while still enforcing the China-only requirement.
func GetChinaIPCheck(client *http.Client) (ip string, isChina bool, err error) {
	ipCheckOnce.Do(func() {
		ipCheckIP, ipCheckChina, ipCheckErr = CheckChinaIP(client)
	})
	return ipCheckIP, ipCheckChina, ipCheckErr
}

// ResetIPCheckCache clears the cached IP result. Useful in tests that
// want to re-check (e.g. after network conditions change).
func ResetIPCheckCache() {
	ipCheckOnce = sync.Once{}
	ipCheckIP = ""
	ipCheckChina = false
	ipCheckErr = nil
}

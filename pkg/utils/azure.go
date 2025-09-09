package utils

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ParseResourceID splits an Azure resource ID for workspace.
func ParseResourceID(id string) (sub, rg, workspace string, err error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", "", "", errors.New("empty resource id")
	}
	parts := strings.Split(id, "/")
	// Expect: /subscriptions/<sub>/resourceGroups/<rg>/providers/Microsoft.OperationalInsights/workspaces/<name>
	if len(parts) < 9 {
		return "", "", "", fmt.Errorf("invalid resource id: %s", id)
	}
	for i := 0; i < len(parts)-1; i++ {
		switch strings.ToLower(parts[i]) {
		case "subscriptions":
			if i+1 < len(parts) {
				sub = parts[i+1]
			}
		case "resourcegroups":
			if i+1 < len(parts) {
				rg = parts[i+1]
			}
		case "workspaces":
			if i+1 < len(parts) {
				workspace = parts[i+1]
			}
		}
	}
	if sub == "" || rg == "" || workspace == "" {
		return "", "", "", fmt.Errorf("failed to parse resource id: %s", id)
	}
	return
}

// ISO8601Duration accepts either Go durations (e.g., 2h45m) or ISO-8601 (PT2H45M) and returns ISO-8601.
func ISO8601Duration(dur string) (string, error) {
	dur = strings.TrimSpace(dur)
	if dur == "" {
		return "", errors.New("empty duration")
	}
	if strings.HasPrefix(strings.ToUpper(dur), "P") {
		// Assume already ISO-8601
		return dur, nil
	}
	d, err := time.ParseDuration(dur)
	if err != nil {
		return "", fmt.Errorf("parse duration: %w", err)
	}
	// Convert to ISO-8601 PT#H#M#S
	secs := int64(d.Seconds())
	if secs < 0 {
		secs = -secs
	}
	h := secs / 3600
	m := (secs % 3600) / 60
	secRem := secs % 60
	return fmt.Sprintf("PT%dH%dM%dS", h, m, secRem), nil
}

// SafeFileName sanitizes table names for filesystem paths.
func SafeFileName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, "/", "_")
	re := regexp.MustCompile(`[^A-Za-z0-9_.\-]`)
	name = re.ReplaceAllString(name, "_")
	if name == "" {
		name = "unnamed"
	}
	return name
}

// ParseISO8601ToDuration parses a subset of ISO8601 durations like PT6H, PT30M, PT1H30M.
func ParseISO8601ToDuration(iso string) (time.Duration, error) {
	iso = strings.ToUpper(strings.TrimSpace(iso))
	if !strings.HasPrefix(iso, "P") {
		return 0, fmt.Errorf("not iso8601: %s", iso)
	}
	// Only support time part for now (PT..)
	i := strings.Index(iso, "T")
	if i == -1 {
		return 0, fmt.Errorf("only time components supported: %s", iso)
	}
	part := iso[i+1:]
	var total time.Duration
	re := regexp.MustCompile(`(?i)(\d+)H`)
	if m := re.FindStringSubmatch(part); len(m) == 2 {
		if v, _ := time.ParseDuration(m[1] + "h"); v > 0 {
			total += v
		}
	}
	re = regexp.MustCompile(`(?i)(\d+)M`)
	if m := re.FindStringSubmatch(part); len(m) == 2 {
		if v, _ := time.ParseDuration(m[1] + "m"); v > 0 {
			total += v
		}
	}
	re = regexp.MustCompile(`(?i)(\d+)S`)
	if m := re.FindStringSubmatch(part); len(m) == 2 {
		if v, _ := time.ParseDuration(m[1] + "s"); v > 0 {
			total += v
		}
	}
	return total, nil
}

// ParseTimeRFC3339 parses RFC3339/RFC3339Nano, returns zero time on failure
func ParseTimeRFC3339(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts
	}
	return time.Time{}
}

package mustgather

import "time"

type Config struct {
	WorkspaceID         string
	Timespan            string
	OutputFile          string
	TableFilter         string
	Profiles            string
	AllTables           bool
	StitchLogs          bool
	StitchIncludeEvents bool
	AIMode              bool
	AIQuery             string
}

type ProfileMap map[string][]string

func GetDefaultProfiles() ProfileMap {
	profileMap := ProfileMap{
		"podLogs":   {"ContainerLogV2", "ContainerLog", "KubeEvents", "KubeMonAgentEvents", "Syslog"},
		"inventory": {"KubePodInventory", "KubeNodeInventory", "KubeServices", "KubePVInventory", "ContainerInventory", "ContainerImageInventory", "ContainerNodeInventory", "KubeHealth"},
		"metrics":   {"InsightsMetrics", "Perf", "Heartbeat"},
		"audit":     {"AKSControlPlane", "AKSAudit", "AKSAuditAdmin"},
	}

	// Alias: aks-debug = podLogs + inventory + metrics
	combined := make([]string, 0, 32)
	seen := map[string]struct{}{}
	for _, k := range []string{"podLogs", "inventory", "metrics"} {
		for _, t := range profileMap[k] {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			combined = append(combined, t)
		}
	}
	profileMap["aks-debug"] = combined

	return profileMap
}

func (c *Config) GenerateDefaultOutputName() string {
	if c.OutputFile == "" {
		return "must-gather-" + time.Now().Format("20060102-150405") + ".tar.gz"
	}
	return c.OutputFile
}

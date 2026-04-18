package dispatch

// Tier holds per-size resource limits and the hard deadline for a single run.
type Tier struct {
	CPURequest            string
	CPULimit              string
	MemoryRequest         string
	MemoryLimit           string
	ActiveDeadlineSeconds int
}

// tiers maps logical size names to resource constraints.
// The xlarge deadline matches the 6-hour admission ceiling.
var tiers = map[string]Tier{
	"small":  {"500m", "1", "1Gi", "2Gi", 3600},
	"medium": {"1", "2", "4Gi", "8Gi", 7200},
	"large":  {"2", "4", "8Gi", "16Gi", 14400},
	"xlarge": {"4", "8", "16Gi", "32Gi", 21600},
}

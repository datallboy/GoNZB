package releasepolicy

type AvailabilityAdjustmentInput struct {
	CompletionPct     float64
	AvailabilityScore float64
	PasswordedKnown   bool
	PasswordedUnknown bool
}

func ClampAvailabilityScore(score float64) float64 {
	switch {
	case score < 0:
		return 0
	case score > 100:
		return 100
	default:
		return score
	}
}

func AvailabilityTier(score float64) string {
	score = ClampAvailabilityScore(score)
	switch {
	case score >= 85:
		return "excellent"
	case score >= 70:
		return "good"
	case score >= 50:
		return "partial"
	default:
		return "low"
	}
}

func AdjustAvailabilityForInspection(in AvailabilityAdjustmentInput) (*float64, string) {
	completionPct := ClampAvailabilityScore(in.CompletionPct)
	availabilityScore := ClampAvailabilityScore(in.AvailabilityScore)

	switch {
	case in.PasswordedUnknown:
		capped := availabilityScore
		unknownCap := completionPct * 0.6
		if unknownCap < 25 {
			unknownCap = 25
		}
		if capped > unknownCap {
			capped = unknownCap
		}
		capped = ClampAvailabilityScore(capped)
		return &capped, AvailabilityTier(capped)
	case in.PasswordedKnown && availabilityScore < completionPct:
		restored := ClampAvailabilityScore(completionPct)
		return &restored, AvailabilityTier(restored)
	default:
		return nil, ""
	}
}

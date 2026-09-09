package service

type ResultPage[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"nextCursor"`
	HasMore    bool    `json:"hasMore"`
	Limit      int     `json:"limit"`
}

func normalizePageLimit(limit int) int {
	const maxLimit = 50
	if limit <= 0 || limit > maxLimit {
		return maxLimit
	}
	return limit
}

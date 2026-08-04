package server

import (
	"sort"
	"time"
)

// BuildOverview computes the dashboard landing-page aggregate.
func (s *Store) BuildOverview(now time.Time) (*Overview, error) {
	ov := &Overview{ByKindTotals: map[string]int{}}

	if err := s.db.QueryRow(`SELECT COUNT(*) FROM installs`).Scan(&ov.Installs); err != nil {
		return nil, err
	}
	weekAgo := now.Add(-7 * 24 * time.Hour).UTC()
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM installs WHERE last_seen > ?`, weekAgo).Scan(&ov.ActiveWeek); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tickets WHERE status = 'open'`).Scan(&ov.OpenTickets); err != nil {
		return nil, err
	}

	// Version distribution.
	rows, err := s.db.Query(`SELECT version, COUNT(*) FROM installs WHERE version != '' GROUP BY version ORDER BY COUNT(*) DESC, version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ov.VersionDist = []VersionCount{}
	for rows.Next() {
		var vc VersionCount
		if err := rows.Scan(&vc.Version, &vc.Count); err != nil {
			return nil, err
		}
		ov.VersionDist = append(ov.VersionDist, vc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Feature usage + service-kind totals come from each install's cached
	// last snapshot. The fleet is small (a CRM, not a telemetry warehouse),
	// so decoding in Go keeps the SQL portable.
	installs, err := s.ListInstalls()
	if err != nil {
		return nil, err
	}
	features := map[string]int{}
	for _, in := range installs {
		if in.Usage == nil {
			continue
		}
		for _, f := range in.Usage.FeaturesUsed {
			features[f]++
		}
		for kind, n := range in.Usage.ServicesByKind {
			ov.ByKindTotals[kind] += n
		}
	}
	ov.FeatureUsage = make([]FeatureCount, 0, len(features))
	for f, c := range features {
		ov.FeatureUsage = append(ov.FeatureUsage, FeatureCount{Feature: f, Count: c})
	}
	sort.Slice(ov.FeatureUsage, func(i, j int) bool {
		if ov.FeatureUsage[i].Count != ov.FeatureUsage[j].Count {
			return ov.FeatureUsage[i].Count > ov.FeatureUsage[j].Count
		}
		return ov.FeatureUsage[i].Feature < ov.FeatureUsage[j].Feature
	})

	ov.RecentTickets, err = s.ListTickets("", 8)
	if err != nil {
		return nil, err
	}
	return ov, nil
}

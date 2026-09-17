package db

import (
	"context"
)

// PhotoViews is one row of the view ranking.
type PhotoViews struct {
	PhotoID string
	Title   string
	Views   int64
}

// AddViews adds a batch of collected counts to today's rows. Counts are
// accumulated, so flushing twice in a day keeps both.
func (d *DB) AddViews(ctx context.Context, counts map[string]int64) error {
	if d == nil || d.pool == nil || len(counts) == 0 {
		return nil
	}
	ids := make([]string, 0, len(counts))
	views := make([]int64, 0, len(counts))
	for id, n := range counts {
		ids = append(ids, id)
		views = append(views, n)
	}
	_, err := d.pool.Exec(ctx,
		`INSERT INTO photo_views (photo_id, day, views)
		 SELECT u.photo_id, CURRENT_DATE, u.views
		 FROM unnest($1::text[], $2::bigint[]) AS u(photo_id, views)
		 ON CONFLICT (photo_id, day)
		 DO UPDATE SET views = photo_views.views + EXCLUDED.views`,
		ids, views,
	)
	return err
}

// TopViews returns the most viewed photos over the last days, title included
// when sync already stored the photo.
func (d *DB) TopViews(ctx context.Context, days, limit int) ([]PhotoViews, error) {
	if d == nil || d.pool == nil {
		return nil, nil
	}
	rows, err := d.pool.Query(ctx,
		`SELECT v.photo_id,
		        COALESCE(MAX(p.info_json->'photo'->'title'->>'_content'), '') AS title,
		        SUM(v.views) AS views
		 FROM photo_views v
		 LEFT JOIN photos p ON p.photo_id = v.photo_id
		 WHERE v.day > CURRENT_DATE - $1::int
		 GROUP BY v.photo_id
		 ORDER BY views DESC
		 LIMIT $2`,
		days, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PhotoViews
	for rows.Next() {
		var r PhotoViews
		if err := rows.Scan(&r.PhotoID, &r.Title, &r.Views); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// TotalViews returns the total number of views over the last days.
func (d *DB) TotalViews(ctx context.Context, days int) (int64, error) {
	if d == nil || d.pool == nil {
		return 0, nil
	}
	var total int64
	err := d.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(views), 0) FROM photo_views WHERE day > CURRENT_DATE - $1::int`,
		days,
	).Scan(&total)
	return total, err
}

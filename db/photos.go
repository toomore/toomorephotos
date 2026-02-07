package db

import (
	"context"
	"encoding/json"
	"math/rand"

	"github.com/jackc/pgx/v5"
	"github.com/toomore/lazyflickrgo/jsonstruct"
)

const orderByPosted = `ORDER BY (info_json->'photo'->'dates'->>'posted')::bigint DESC NULLS LAST`

// GetPhoto returns PhotosGetInfo, width, height for a photo. ok is false if not found.
func (d *DB) GetPhoto(ctx context.Context, photoID string) (info jsonstruct.PhotosGetInfo, width, height int64, ok bool) {
	if d == nil || d.pool == nil {
		return jsonstruct.PhotosGetInfo{}, 0, 0, false
	}
	var infoJSON []byte
	err := d.pool.QueryRow(ctx,
		`SELECT info_json, width, height FROM photos WHERE photo_id = $1`,
		photoID,
	).Scan(&infoJSON, &width, &height)
	if err == pgx.ErrNoRows {
		return jsonstruct.PhotosGetInfo{}, 0, 0, false
	}
	if err != nil {
		return jsonstruct.PhotosGetInfo{}, 0, 0, false
	}
	if err := json.Unmarshal(infoJSON, &info); err != nil {
		return jsonstruct.PhotosGetInfo{}, 0, 0, false
	}
	return info, width, height, true
}

// UpsertPhoto inserts or updates a photo and its tags.
func (d *DB) UpsertPhoto(ctx context.Context, photoID string, info jsonstruct.PhotosGetInfo, width, height int64) error {
	if d == nil || d.pool == nil {
		return nil
	}
	infoJSON, err := json.Marshal(info)
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(ctx,
		`INSERT INTO photos (photo_id, info_json, width, height, fetched_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (photo_id) DO UPDATE SET
		   info_json = EXCLUDED.info_json,
		   width = EXCLUDED.width,
		   height = EXCLUDED.height,
		   fetched_at = NOW()`,
		photoID, infoJSON, width, height,
	)
	if err != nil {
		return err
	}
	// Replace tags
	_, err = d.pool.Exec(ctx, `DELETE FROM photo_tags WHERE photo_id = $1`, photoID)
	if err != nil {
		return err
	}
	for _, t := range info.Photo.Tags.Tag {
		if t.Raw == "" {
			continue
		}
		_, err = d.pool.Exec(ctx,
			`INSERT INTO photo_tags (photo_id, tag) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			photoID, t.Raw,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// photoInfoToPhoto converts PhotosGetInfo.Photo to jsonstruct.Photo for list display.
func photoInfoToPhoto(info *jsonstruct.PhotosGetInfo) jsonstruct.Photo {
	p := info.Photo
	return jsonstruct.Photo{
		ID:       p.ID,
		Owner:    p.Owner.Nsid,
		Title:    p.Title.Content,
		Secret:   p.Secret,
		Server:   p.Server,
		Farm:     p.Farm,
		Ispublic: 1, // PhotosGetInfo may not have this; assume public
	}
}

// GetPhotosByTag returns photos with the given tag, ordered by date-posted-desc.
func (d *DB) GetPhotosByTag(ctx context.Context, tag string) ([]jsonstruct.Photo, error) {
	if d == nil || d.pool == nil {
		return nil, nil
	}
	rows, err := d.pool.Query(ctx,
		`SELECT p.info_json FROM photos p
		 INNER JOIN photo_tags pt ON p.photo_id = pt.photo_id
		 WHERE pt.tag = $1 `+orderByPosted,
		tag,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []jsonstruct.Photo
	for rows.Next() {
		var infoJSON []byte
		if err := rows.Scan(&infoJSON); err != nil {
			return nil, err
		}
		var info jsonstruct.PhotosGetInfo
		if err := json.Unmarshal(infoJSON, &info); err != nil {
			continue
		}
		result = append(result, photoInfoToPhoto(&info))
	}
	return result, nil
}

// GetAllPhotos returns all photos ordered by date-posted-desc.
func (d *DB) GetAllPhotos(ctx context.Context) ([]jsonstruct.Photo, error) {
	if d == nil || d.pool == nil {
		return nil, nil
	}
	rows, err := d.pool.Query(ctx,
		`SELECT info_json FROM photos `+orderByPosted,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []jsonstruct.Photo
	for rows.Next() {
		var infoJSON []byte
		if err := rows.Scan(&infoJSON); err != nil {
			return nil, err
		}
		var info jsonstruct.PhotosGetInfo
		if err := json.Unmarshal(infoJSON, &info); err != nil {
			continue
		}
		result = append(result, photoInfoToPhoto(&info))
	}
	return result, nil
}

// GetRelatedPhotos returns related photos: same tags first, then other tags, shuffled.
func (d *DB) GetRelatedPhotos(ctx context.Context, excludePhotoID string, tagRaws []string, allTags []string, limit int) ([]jsonstruct.Photo, error) {
	if d == nil || d.pool == nil || len(tagRaws) == 0 {
		return nil, nil
	}
	const sameTagLimit = 8
	const otherTagLimit = 4

	// 1. Same-tag results
	tagSet := make(map[string]bool)
	for _, t := range tagRaws {
		tagSet[t] = true
	}
	var sameTag []jsonstruct.Photo
	for _, tag := range tagRaws {
		photos, err := d.GetPhotosByTag(ctx, tag)
		if err != nil {
			continue
		}
		for _, p := range photos {
			if p.ID != excludePhotoID && p.Ispublic != 0 {
				sameTag = append(sameTag, p)
			}
		}
	}
	// Dedupe by ID
	seen := make(map[string]bool)
	var deduped []jsonstruct.Photo
	for _, p := range sameTag {
		if !seen[p.ID] {
			seen[p.ID] = true
			deduped = append(deduped, p)
		}
	}
	sameTag = deduped
	rand.Shuffle(len(sameTag), func(i, j int) { sameTag[i], sameTag[j] = sameTag[j], sameTag[i] })
	if len(sameTag) > sameTagLimit {
		sameTag = sameTag[:sameTagLimit]
	}

	// 2. Other tags
	var otherTags []string
	for _, t := range allTags {
		if !tagSet[t] {
			otherTags = append(otherTags, t)
		}
	}
	var otherTag []jsonstruct.Photo
	if len(otherTags) > 0 {
		var h uint32
		for _, c := range excludePhotoID {
			h = h*31 + uint32(c)
		}
		pickTag := otherTags[int(h)%len(otherTags)]
		photos, err := d.GetPhotosByTag(ctx, pickTag)
		if err == nil {
			for _, p := range photos {
				if p.ID != excludePhotoID && p.Ispublic != 0 && !seen[p.ID] {
					otherTag = append(otherTag, p)
					seen[p.ID] = true
					if len(otherTag) >= otherTagLimit {
						break
					}
				}
			}
		}
	}

	// 3. Merge and shuffle
	merged := append(sameTag, otherTag...)
	rand.Shuffle(len(merged), func(i, j int) { merged[i], merged[j] = merged[j], merged[i] })
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

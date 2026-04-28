package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrTagExists   = errors.New("tag already exists")
	ErrTagNotFound = errors.New("tag not found")
	ErrUnknownTag  = errors.New("one or more tags do not exist")
)

type Tag struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"createdAt"`
	VideoCount int       `json:"videoCount"`
}

type VideoRecord struct {
	ID              string    `json:"id"`
	VideoID         string    `json:"videoId"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Author          string    `json:"author"`
	AuthorID        string    `json:"authorId"`
	ShareURL        string    `json:"shareUrl"`
	OriginalURL     string    `json:"originalUrl"`
	ContentType     string    `json:"contentType"`
	CoverURL        string    `json:"coverUrl"`
	CoverSourceURL  string    `json:"coverSourceUrl"`
	CoverLocalFile  string    `json:"coverLocalFile"`
	CoverLocalURL   string    `json:"coverLocalUrl"`
	VideoURI        string    `json:"videoUri"`
	DownloadURL     string    `json:"downloadUrl"`
	WatermarkURL    string    `json:"watermarkUrl"`
	VideoWidth      int64     `json:"videoWidth"`
	VideoHeight     int64     `json:"videoHeight"`
	Duration        float64   `json:"duration"`
	LikeCount       int64     `json:"likeCount"`
	CommentCount    int64     `json:"commentCount"`
	ShareCount      int64     `json:"shareCount"`
	CollectCount    int64     `json:"collectCount"`
	Rating          int       `json:"rating"`
	LocalFile       string    `json:"localFile"`
	LocalURL        string    `json:"localUrl"`
	FileSize        int64     `json:"fileSize"`
	SavedAt         time.Time `json:"savedAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
	LastSourceInput string    `json:"lastSourceInput"`
	Tags            []Tag     `json:"tags"`
}

type ListFilter struct {
	Mode   string
	Rating int
	TagIDs []int64
}

type Store struct {
	db *sql.DB
}

type rowScanner interface {
	Scan(dest ...any) error
}

const schema = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS videos (
	video_id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	description TEXT NOT NULL,
	author TEXT NOT NULL,
	author_id TEXT NOT NULL,
	share_url TEXT NOT NULL,
	original_url TEXT NOT NULL,
	content_type TEXT NOT NULL,
	cover_url TEXT NOT NULL,
	cover_source_url TEXT NOT NULL DEFAULT '',
	cover_local_file TEXT NOT NULL DEFAULT '',
	cover_local_url TEXT NOT NULL DEFAULT '',
	video_uri TEXT NOT NULL,
	download_url TEXT NOT NULL,
	watermark_url TEXT NOT NULL,
	video_width INTEGER NOT NULL DEFAULT 0,
	video_height INTEGER NOT NULL DEFAULT 0,
	duration REAL NOT NULL DEFAULT 0,
	like_count INTEGER NOT NULL DEFAULT 0,
	comment_count INTEGER NOT NULL DEFAULT 0,
	share_count INTEGER NOT NULL DEFAULT 0,
	collect_count INTEGER NOT NULL DEFAULT 0,
	rating INTEGER NOT NULL DEFAULT 0,
	local_file TEXT NOT NULL,
	local_url TEXT NOT NULL,
	file_size INTEGER NOT NULL DEFAULT 0,
	last_source_input TEXT NOT NULL,
	saved_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tags (
	tag_id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL COLLATE NOCASE UNIQUE,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS video_tags (
	video_id TEXT NOT NULL,
	tag_id INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	PRIMARY KEY (video_id, tag_id),
	FOREIGN KEY (video_id) REFERENCES videos(video_id) ON DELETE CASCADE,
	FOREIGN KEY (tag_id) REFERENCES tags(tag_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_videos_saved_at ON videos (saved_at DESC);
CREATE INDEX IF NOT EXISTS idx_videos_rating_saved_at ON videos (rating, saved_at DESC);
CREATE INDEX IF NOT EXISTS idx_tags_name ON tags (name COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_video_tags_tag_id ON video_tags (tag_id);
CREATE INDEX IF NOT EXISTS idx_video_tags_video_id ON video_tags (video_id);
`

var requiredColumns = []struct {
	Name       string
	Definition string
}{
	{Name: "cover_source_url", Definition: "TEXT NOT NULL DEFAULT ''"},
	{Name: "cover_local_file", Definition: "TEXT NOT NULL DEFAULT ''"},
	{Name: "cover_local_url", Definition: "TEXT NOT NULL DEFAULT ''"},
	{Name: "video_width", Definition: "INTEGER NOT NULL DEFAULT 0"},
	{Name: "video_height", Definition: "INTEGER NOT NULL DEFAULT 0"},
}

func New(dbPath, legacyJSONPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize sqlite schema: %w", err)
	}

	store := &Store{db: db}
	if err := store.ensureSchemaColumns(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrateLegacyJSON(legacyJSONPath); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) ensureSchemaColumns() error {
	existing := make(map[string]bool)

	rows, err := s.db.Query(`PRAGMA table_info(videos)`)
	if err != nil {
		return fmt.Errorf("inspect sqlite schema: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)

		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			return fmt.Errorf("scan sqlite schema info: %w", err)
		}

		existing[name] = true
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate sqlite schema info: %w", err)
	}

	for _, column := range requiredColumns {
		if existing[column.Name] {
			continue
		}

		query := fmt.Sprintf("ALTER TABLE videos ADD COLUMN %s %s", column.Name, column.Definition)
		if _, err := s.db.Exec(query); err != nil {
			return fmt.Errorf("add sqlite column %s: %w", column.Name, err)
		}
	}

	return nil
}

func (s *Store) List(filter ListFilter) ([]VideoRecord, error) {
	query := strings.Builder{}
	query.WriteString(`
SELECT
	video_id,
	title,
	description,
	author,
	author_id,
	share_url,
	original_url,
	content_type,
	cover_url,
	cover_source_url,
	cover_local_file,
	cover_local_url,
	video_uri,
	download_url,
	watermark_url,
	video_width,
	video_height,
	duration,
	like_count,
	comment_count,
	share_count,
	collect_count,
	rating,
	local_file,
	local_url,
	file_size,
	last_source_input,
	saved_at,
	updated_at
FROM videos`)

	whereClauses := make([]string, 0, 2)
	args := make([]any, 0, len(filter.TagIDs)+2)

	switch filter.Mode {
	case "unrated":
		whereClauses = append(whereClauses, `rating = 0`)
	case "rated":
		whereClauses = append(whereClauses, `rating > 0`)
	case "exact":
		whereClauses = append(whereClauses, `rating = ?`)
		args = append(args, filter.Rating)
	}

	tagIDs := normalizeTagIDs(filter.TagIDs)
	if len(tagIDs) > 0 {
		placeholders := placeholders(len(tagIDs))
		whereClauses = append(whereClauses, fmt.Sprintf(`video_id IN (
SELECT video_id
FROM video_tags
WHERE tag_id IN (%s)
GROUP BY video_id
HAVING COUNT(DISTINCT tag_id) = ?
)`, placeholders))
		for _, tagID := range tagIDs {
			args = append(args, tagID)
		}
		args = append(args, len(tagIDs))
	}

	if len(whereClauses) > 0 {
		query.WriteString(` WHERE `)
		query.WriteString(strings.Join(whereClauses, ` AND `))
	}

	query.WriteString(` ORDER BY saved_at DESC`)

	rows, err := s.db.Query(query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query videos: %w", err)
	}
	defer rows.Close()

	records := make([]VideoRecord, 0)
	for rows.Next() {
		record, err := scanVideoRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate videos: %w", err)
	}

	return s.attachTags(records)
}

func (s *Store) Get(videoID string) (VideoRecord, error) {
	row := s.db.QueryRow(`
SELECT
	video_id,
	title,
	description,
	author,
	author_id,
	share_url,
	original_url,
	content_type,
	cover_url,
	cover_source_url,
	cover_local_file,
	cover_local_url,
	video_uri,
	download_url,
	watermark_url,
	video_width,
	video_height,
	duration,
	like_count,
	comment_count,
	share_count,
	collect_count,
	rating,
	local_file,
	local_url,
	file_size,
	last_source_input,
	saved_at,
	updated_at
FROM videos
WHERE video_id = ?`, videoID)

	record, err := scanVideoRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return VideoRecord{}, os.ErrNotExist
	}
	if err != nil {
		return VideoRecord{}, fmt.Errorf("get video %s: %w", videoID, err)
	}

	records, err := s.attachTags([]VideoRecord{record})
	if err != nil {
		return VideoRecord{}, err
	}
	if len(records) == 0 {
		return VideoRecord{}, os.ErrNotExist
	}

	return records[0], nil
}

func (s *Store) Upsert(record VideoRecord) (VideoRecord, error) {
	existing, err := s.Get(record.VideoID)
	switch {
	case err == nil:
		record.SavedAt = existing.SavedAt
		if record.Rating == 0 {
			record.Rating = existing.Rating
		}
		if len(record.Tags) == 0 {
			record.Tags = existing.Tags
		}
	case errors.Is(err, os.ErrNotExist):
		if record.SavedAt.IsZero() {
			record.SavedAt = time.Now().UTC()
		}
	default:
		return VideoRecord{}, err
	}

	record = normalizeRecord(record)
	record.UpdatedAt = time.Now().UTC()

	if err := s.upsertRecord(record); err != nil {
		return VideoRecord{}, err
	}

	return s.Get(record.VideoID)
}

func (s *Store) UpdateRating(videoID string, rating int) (VideoRecord, error) {
	record, err := s.Get(videoID)
	if err != nil {
		return VideoRecord{}, err
	}

	record.Rating = rating
	record.UpdatedAt = time.Now().UTC()

	if err := s.upsertRecord(record); err != nil {
		return VideoRecord{}, err
	}

	return s.Get(videoID)
}

func (s *Store) SetVideoTags(videoID string, tagIDs []int64) (VideoRecord, error) {
	if _, err := s.Get(videoID); err != nil {
		return VideoRecord{}, err
	}

	tagIDs = normalizeTagIDs(tagIDs)

	tx, err := s.db.Begin()
	if err != nil {
		return VideoRecord{}, fmt.Errorf("begin set video tags transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if len(tagIDs) > 0 {
		query := fmt.Sprintf(`SELECT COUNT(*) FROM tags WHERE tag_id IN (%s)`, placeholders(len(tagIDs)))
		args := make([]any, 0, len(tagIDs))
		for _, tagID := range tagIDs {
			args = append(args, tagID)
		}

		var count int
		if err := tx.QueryRow(query, args...).Scan(&count); err != nil {
			return VideoRecord{}, fmt.Errorf("validate tags: %w", err)
		}
		if count != len(tagIDs) {
			return VideoRecord{}, ErrUnknownTag
		}
	}

	if _, err := tx.Exec(`DELETE FROM video_tags WHERE video_id = ?`, videoID); err != nil {
		return VideoRecord{}, fmt.Errorf("clear video tags: %w", err)
	}

	if len(tagIDs) > 0 {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		for _, tagID := range tagIDs {
			if _, err := tx.Exec(`INSERT INTO video_tags (video_id, tag_id, created_at) VALUES (?, ?, ?)`, videoID, tagID, now); err != nil {
				return VideoRecord{}, fmt.Errorf("attach tag %d to video %s: %w", tagID, videoID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return VideoRecord{}, fmt.Errorf("commit video tags transaction: %w", err)
	}

	return s.Get(videoID)
}

func (s *Store) Delete(videoID string) error {
	result, err := s.db.Exec(`DELETE FROM videos WHERE video_id = ?`, videoID)
	if err != nil {
		return fmt.Errorf("delete video %s: %w", videoID, err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check deleted rows: %w", err)
	}
	if affected == 0 {
		return os.ErrNotExist
	}

	return nil
}

func (s *Store) Count() (int, error) {
	row := s.db.QueryRow(`SELECT COUNT(*) FROM videos`)

	var count int
	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("count videos: %w", err)
	}

	return count, nil
}

func (s *Store) ListTags() ([]Tag, error) {
	rows, err := s.db.Query(`
SELECT
	t.tag_id,
	t.name,
	t.created_at,
	COUNT(vt.video_id) AS video_count
FROM tags t
LEFT JOIN video_tags vt ON vt.tag_id = t.tag_id
GROUP BY t.tag_id, t.name, t.created_at
ORDER BY t.name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, fmt.Errorf("query tags: %w", err)
	}
	defer rows.Close()

	tags := make([]Tag, 0)
	for rows.Next() {
		var tag Tag
		var createdAt string
		if err := rows.Scan(&tag.ID, &tag.Name, &createdAt, &tag.VideoCount); err != nil {
			return nil, fmt.Errorf("scan tag row: %w", err)
		}
		if tag.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
			return nil, fmt.Errorf("parse tag createdAt: %w", err)
		}
		tags = append(tags, tag)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tags: %w", err)
	}

	return tags, nil
}

func (s *Store) CreateTag(name string) (Tag, error) {
	name = normalizeTagName(name)
	if name == "" {
		return Tag{}, errors.New("tag name is required")
	}

	now := time.Now().UTC()
	result, err := s.db.Exec(`INSERT INTO tags (name, created_at) VALUES (?, ?)`, name, now.Format(time.RFC3339Nano))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
			return Tag{}, ErrTagExists
		}
		return Tag{}, fmt.Errorf("create tag %q: %w", name, err)
	}

	tagID, err := result.LastInsertId()
	if err != nil {
		return Tag{}, fmt.Errorf("get tag insert id: %w", err)
	}

	return Tag{
		ID:         tagID,
		Name:       name,
		CreatedAt:  now,
		VideoCount: 0,
	}, nil
}

func (s *Store) DeleteTag(tagID int64) error {
	result, err := s.db.Exec(`DELETE FROM tags WHERE tag_id = ?`, tagID)
	if err != nil {
		return fmt.Errorf("delete tag %d: %w", tagID, err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check deleted tags rows: %w", err)
	}
	if affected == 0 {
		return ErrTagNotFound
	}

	return nil
}

func (s *Store) upsertRecord(record VideoRecord) error {
	record = normalizeRecord(record)

	_, err := s.db.Exec(`
INSERT INTO videos (
	video_id,
	title,
	description,
	author,
	author_id,
	share_url,
	original_url,
	content_type,
	cover_url,
	cover_source_url,
	cover_local_file,
	cover_local_url,
	video_uri,
	download_url,
	watermark_url,
	video_width,
	video_height,
	duration,
	like_count,
	comment_count,
	share_count,
	collect_count,
	rating,
	local_file,
	local_url,
	file_size,
	last_source_input,
	saved_at,
	updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(video_id) DO UPDATE SET
	title = excluded.title,
	description = excluded.description,
	author = excluded.author,
	author_id = excluded.author_id,
	share_url = excluded.share_url,
	original_url = excluded.original_url,
	content_type = excluded.content_type,
	cover_url = excluded.cover_url,
	cover_source_url = excluded.cover_source_url,
	cover_local_file = excluded.cover_local_file,
	cover_local_url = excluded.cover_local_url,
	video_uri = excluded.video_uri,
	download_url = excluded.download_url,
	watermark_url = excluded.watermark_url,
	video_width = excluded.video_width,
	video_height = excluded.video_height,
	duration = excluded.duration,
	like_count = excluded.like_count,
	comment_count = excluded.comment_count,
	share_count = excluded.share_count,
	collect_count = excluded.collect_count,
	rating = excluded.rating,
	local_file = excluded.local_file,
	local_url = excluded.local_url,
	file_size = excluded.file_size,
	last_source_input = excluded.last_source_input,
	saved_at = excluded.saved_at,
	updated_at = excluded.updated_at
`, record.VideoID, record.Title, record.Description, record.Author, record.AuthorID, record.ShareURL, record.OriginalURL, record.ContentType, record.CoverURL, record.CoverSourceURL, record.CoverLocalFile, record.CoverLocalURL, record.VideoURI, record.DownloadURL, record.WatermarkURL, record.VideoWidth, record.VideoHeight, record.Duration, record.LikeCount, record.CommentCount, record.ShareCount, record.CollectCount, record.Rating, record.LocalFile, record.LocalURL, record.FileSize, record.LastSourceInput, record.SavedAt.Format(time.RFC3339Nano), record.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert video %s: %w", record.VideoID, err)
	}

	return nil
}

func (s *Store) migrateLegacyJSON(legacyPath string) error {
	if strings.TrimSpace(legacyPath) == "" {
		return nil
	}

	count, err := s.Count()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	payload, err := os.ReadFile(legacyPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read legacy metadata file: %w", err)
	}

	var records []VideoRecord
	if err := json.Unmarshal(payload, &records); err != nil {
		return fmt.Errorf("decode legacy metadata file: %w", err)
	}

	for _, record := range records {
		record = normalizeRecord(record)
		if record.SavedAt.IsZero() {
			record.SavedAt = time.Now().UTC()
		}
		if record.UpdatedAt.IsZero() {
			record.UpdatedAt = record.SavedAt
		}

		if err := s.upsertRecord(record); err != nil {
			return fmt.Errorf("migrate legacy record %s: %w", record.VideoID, err)
		}
	}

	return nil
}

func (s *Store) attachTags(records []VideoRecord) ([]VideoRecord, error) {
	if len(records) == 0 {
		return records, nil
	}

	videoIDs := make([]string, 0, len(records))
	for _, record := range records {
		videoIDs = append(videoIDs, record.VideoID)
	}

	tagsByVideo, err := s.loadTagsForVideoIDs(videoIDs)
	if err != nil {
		return nil, err
	}

	for i := range records {
		records[i].Tags = tagsByVideo[records[i].VideoID]
		if records[i].Tags == nil {
			records[i].Tags = []Tag{}
		}
	}

	return records, nil
}

func (s *Store) loadTagsForVideoIDs(videoIDs []string) (map[string][]Tag, error) {
	tagMap := make(map[string][]Tag, len(videoIDs))
	videoIDs = normalizeVideoIDs(videoIDs)
	if len(videoIDs) == 0 {
		return tagMap, nil
	}

	query := fmt.Sprintf(`
SELECT
	vt.video_id,
	t.tag_id,
	t.name,
	t.created_at
FROM video_tags vt
JOIN tags t ON t.tag_id = vt.tag_id
WHERE vt.video_id IN (%s)
ORDER BY t.name COLLATE NOCASE ASC`, placeholders(len(videoIDs)))

	args := make([]any, 0, len(videoIDs))
	for _, videoID := range videoIDs {
		args = append(args, videoID)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query tags for videos: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			videoID   string
			tag       Tag
			createdAt string
		)

		if err := rows.Scan(&videoID, &tag.ID, &tag.Name, &createdAt); err != nil {
			return nil, fmt.Errorf("scan video tag row: %w", err)
		}
		if tag.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
			return nil, fmt.Errorf("parse video tag createdAt: %w", err)
		}
		tagMap[videoID] = append(tagMap[videoID], tag)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate video tags: %w", err)
	}

	return tagMap, nil
}

func scanVideoRecord(scanner rowScanner) (VideoRecord, error) {
	var record VideoRecord
	var savedAt string
	var updatedAt string

	err := scanner.Scan(
		&record.VideoID,
		&record.Title,
		&record.Description,
		&record.Author,
		&record.AuthorID,
		&record.ShareURL,
		&record.OriginalURL,
		&record.ContentType,
		&record.CoverURL,
		&record.CoverSourceURL,
		&record.CoverLocalFile,
		&record.CoverLocalURL,
		&record.VideoURI,
		&record.DownloadURL,
		&record.WatermarkURL,
		&record.VideoWidth,
		&record.VideoHeight,
		&record.Duration,
		&record.LikeCount,
		&record.CommentCount,
		&record.ShareCount,
		&record.CollectCount,
		&record.Rating,
		&record.LocalFile,
		&record.LocalURL,
		&record.FileSize,
		&record.LastSourceInput,
		&savedAt,
		&updatedAt,
	)
	if err != nil {
		return VideoRecord{}, err
	}

	record.ID = record.VideoID
	record.Tags = []Tag{}

	if record.SavedAt, err = time.Parse(time.RFC3339Nano, savedAt); err != nil {
		return VideoRecord{}, fmt.Errorf("parse savedAt: %w", err)
	}
	if record.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return VideoRecord{}, fmt.Errorf("parse updatedAt: %w", err)
	}

	return record, nil
}

func normalizeRecord(record VideoRecord) VideoRecord {
	record.ID = firstNonEmpty(record.ID, record.VideoID)
	record.VideoID = firstNonEmpty(record.VideoID, record.ID)
	record.Title = strings.TrimSpace(record.Title)
	record.Description = strings.TrimSpace(record.Description)
	record.Author = strings.TrimSpace(record.Author)
	record.AuthorID = strings.TrimSpace(record.AuthorID)
	record.ShareURL = strings.TrimSpace(record.ShareURL)
	record.OriginalURL = strings.TrimSpace(record.OriginalURL)
	record.ContentType = strings.TrimSpace(record.ContentType)
	record.CoverSourceURL = strings.TrimSpace(firstNonEmpty(record.CoverSourceURL, record.CoverURL))
	record.CoverLocalFile = filepath.ToSlash(strings.TrimSpace(record.CoverLocalFile))
	record.CoverLocalURL = strings.TrimSpace(record.CoverLocalURL)
	record.CoverURL = strings.TrimSpace(firstNonEmpty(record.CoverLocalURL, record.CoverURL, record.CoverSourceURL))
	record.VideoURI = strings.TrimSpace(record.VideoURI)
	record.DownloadURL = strings.TrimSpace(record.DownloadURL)
	record.WatermarkURL = strings.TrimSpace(record.WatermarkURL)
	record.LocalFile = filepath.ToSlash(strings.TrimSpace(record.LocalFile))
	record.LocalURL = strings.TrimSpace(record.LocalURL)
	record.LastSourceInput = strings.TrimSpace(record.LastSourceInput)
	record.Tags = normalizeTags(record.Tags)
	return record
}

func normalizeTagName(name string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
}

func normalizeTagIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}

	seen := make(map[int64]bool, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func normalizeVideoIDs(videoIDs []string) []string {
	if len(videoIDs) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(videoIDs))
	result := make([]string, 0, len(videoIDs))
	for _, videoID := range videoIDs {
		videoID = strings.TrimSpace(videoID)
		if videoID == "" || seen[videoID] {
			continue
		}
		seen[videoID] = true
		result = append(result, videoID)
	}
	return result
}

func normalizeTags(tags []Tag) []Tag {
	if len(tags) == 0 {
		return []Tag{}
	}

	seen := make(map[int64]bool, len(tags))
	result := make([]Tag, 0, len(tags))
	for _, tag := range tags {
		if tag.ID <= 0 || seen[tag.ID] {
			continue
		}
		tag.Name = normalizeTagName(tag.Name)
		seen[tag.ID] = true
		result = append(result, tag)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

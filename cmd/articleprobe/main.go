package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	settingsstore "github.com/datallboy/gonzb/internal/store/settings"
)

type articleRef struct {
	messageID     string
	articleNumber int64
}

type nzbExportFile struct {
	ID        int64
	BinaryID  int64
	FileName  string
	Subject   string
	Poster    string
	DateUnix  int64
	Groups    []string
	SizeBytes int64
	Index     int
	Segments  []nzb.Segment
}

func main() {
	var (
		configPath       string
		serverID         string
		nntpProviderID   string
		providerID       string
		group            string
		messageID        string
		articleNum       int64
		xoverStart       int64
		xoverEnd         int64
		xoverOut         string
		xoverGrep        string
		bodyBytes        int64
		articleBytes     int64
		headLines        int
		releaseID        string
		releaseFamilyKey string
		binaryID         int64
		outNZB           string
		probeSetYEnc     bool
		setBodyBytes     int64
	)

	flag.StringVar(&configPath, "config", "config.yaml", "config file path")
	flag.StringVar(&serverID, "server", "", "NNTP server id from config.yaml/runtime settings (defaults to first server)")
	flag.StringVar(&nntpProviderID, "nntp-provider-id", "", "NNTP provider/server id to use; alias for --server")
	flag.StringVar(&providerID, "provider-id", "", "NNTP provider/server id to use; alias for --server")
	flag.StringVar(&group, "group", "", "newsgroup to select before article-number operations")
	flag.StringVar(&messageID, "message-id", "", "message-id to inspect")
	flag.Int64Var(&articleNum, "article-number", 0, "article number to inspect within --group")
	flag.Int64Var(&xoverStart, "xover-start", 0, "export XOVER rows starting at this article number within --group")
	flag.Int64Var(&xoverEnd, "xover-end", 0, "export XOVER rows ending at this article number within --group")
	flag.StringVar(&xoverOut, "xover-out", "", "write XOVER range output to this path; defaults to stdout")
	flag.StringVar(&xoverGrep, "xover-grep", "", "optional regexp filter to print matching XOVER rows to stderr during range export")
	flag.Int64Var(&bodyBytes, "body-bytes", 4096, "max BODY bytes to print; 0 disables BODY")
	flag.Int64Var(&articleBytes, "article-bytes", 8192, "max ARTICLE bytes to print; 0 disables ARTICLE")
	flag.IntVar(&headLines, "head-lines", 200, "max HEAD lines to print")
	flag.StringVar(&releaseID, "release-id", "", "export an NZB for a formed indexer release id")
	flag.StringVar(&releaseFamilyKey, "release-family-key", "", "export an NZB for binaries with this release_family_key")
	flag.Int64Var(&binaryID, "binary-id", 0, "export an NZB for one binary id")
	flag.StringVar(&outNZB, "out-nzb", "", "write exported NZB to this path; defaults to stdout for export modes")
	flag.BoolVar(&probeSetYEnc, "probe-set-yenc", false, "for export modes, fetch BODY prefixes and print yEnc header names for segments")
	flag.Int64Var(&setBodyBytes, "set-body-bytes", 8192, "BODY prefix bytes to fetch per segment for --probe-set-yenc")
	flag.Parse()

	serverID, err := resolveArticleProbeServerID(serverID, nntpProviderID, providerID)
	fatalIf(err)

	cfg, err := config.Load(configPath)
	fatalIf(err)
	cfg, err = withRuntimeServers(cfg)
	fatalIf(err)

	if xoverStart > 0 || xoverEnd > 0 {
		fatalIf(runXOverExport(context.Background(), cfg, serverID, group, xoverStart, xoverEnd, xoverOut, xoverGrep))
		return
	}

	if isExportMode(releaseID, releaseFamilyKey, binaryID) {
		fatalIf(runNZBExport(context.Background(), cfg, serverID, releaseID, releaseFamilyKey, binaryID, outNZB, probeSetYEnc, setBodyBytes))
		return
	}

	ref, err := normalizeArticleRef(messageID, articleNum)
	fatalIf(err)

	server, err := chooseServer(cfg, serverID)
	fatalIf(err)

	conn, err := dialServer(server)
	fatalIf(err)
	defer conn.Close()

	fmt.Printf("server: %s (%s:%d tls=%t)\n", server.ID, server.Host, server.Port, server.TLS)

	if strings.TrimSpace(group) != "" {
		stats, err := selectGroup(conn, group)
		fatalIf(err)
		fmt.Printf("group: %s count=%d low=%d high=%d\n", stats.group, stats.count, stats.low, stats.high)
		if ref.articleNumber > 0 {
			if err := printStat(conn, ref); err != nil {
				fmt.Printf("stat: %v\n", err)
			}
			if err := printXOver(conn, ref.articleNumber); err != nil {
				fmt.Printf("xover: %v\n", err)
			}
		}
	}

	if err := printHead(conn, ref, headLines); err != nil {
		fmt.Printf("head: %v\n", err)
	}
	if bodyBytes > 0 {
		if err := printBody(conn, ref, bodyBytes); err != nil {
			fmt.Printf("body: %v\n", err)
		}
	}
	if articleBytes > 0 {
		if err := printArticle(conn, ref, articleBytes); err != nil {
			fmt.Printf("article: %v\n", err)
		}
	}
}

func isExportMode(releaseID, releaseFamilyKey string, binaryID int64) bool {
	return strings.TrimSpace(releaseID) != "" || strings.TrimSpace(releaseFamilyKey) != "" || binaryID > 0
}

func runXOverExport(ctx context.Context, cfg *config.Config, serverID, group string, start, end int64, outPath, grepExpr string) error {
	group = strings.TrimSpace(group)
	if group == "" {
		return fmt.Errorf("--group is required for XOVER export")
	}
	if start <= 0 || end <= 0 {
		return fmt.Errorf("both --xover-start and --xover-end are required")
	}
	if end < start {
		return fmt.Errorf("--xover-end must be greater than or equal to --xover-start")
	}
	var grepRE *regexp.Regexp
	if strings.TrimSpace(grepExpr) != "" {
		var err error
		grepRE, err = regexp.Compile(grepExpr)
		if err != nil {
			return fmt.Errorf("compile --xover-grep: %w", err)
		}
	}

	server, err := chooseServer(cfg, serverID)
	if err != nil {
		return err
	}

	conn, err := dialServer(server)
	if err != nil {
		return err
	}
	defer conn.Close()

	stats, err := selectGroup(conn, group)
	if err != nil {
		return err
	}

	lines, err := fetchXOverLines(conn, start, end)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "XOVER %d-%d\n", start, end)
	buf.WriteString("224 Overview Information Follows\n")
	for _, line := range lines {
		buf.WriteString(line)
		buf.WriteByte('\n')
	}

	if err := writeOutputFile(outPath, buf.Bytes()); err != nil {
		return err
	}

	dest := strings.TrimSpace(outPath)
	if dest == "" || dest == "-" {
		dest = "stdout"
	}
	fmt.Fprintf(os.Stderr, "exported xover rows=%d group=%s low=%d high=%d output=%s\n", len(lines), stats.group, stats.low, stats.high, dest)
	if grepRE != nil {
		matches := 0
		for _, line := range lines {
			if grepRE.MatchString(line) {
				fmt.Fprintln(os.Stderr, line)
				matches++
			}
		}
		fmt.Fprintf(os.Stderr, "xover-grep matches=%d pattern=%q\n", matches, grepExpr)
	}
	return nil
}

func runNZBExport(ctx context.Context, cfg *config.Config, serverID, releaseID, releaseFamilyKey string, binaryID int64, outPath string, probeYEnc bool, prefixBytes int64) error {
	selected := 0
	if strings.TrimSpace(releaseID) != "" {
		selected++
	}
	if strings.TrimSpace(releaseFamilyKey) != "" {
		selected++
	}
	if binaryID > 0 {
		selected++
	}
	if selected != 1 {
		return fmt.Errorf("exactly one of --release-id, --release-family-key, or --binary-id is required for NZB export")
	}
	if cfg == nil || strings.TrimSpace(cfg.Store.PGDSN) == "" {
		return fmt.Errorf("store.pg_dsn is required for NZB export")
	}

	store, err := pgindex.NewStore(cfg.Store.PGDSN)
	if err != nil {
		return fmt.Errorf("open pgindex store: %w", err)
	}
	defer store.Close()

	var title string
	var files []nzbExportFile
	switch {
	case strings.TrimSpace(releaseID) != "":
		title, files, err = loadNZBExportRelease(ctx, store, releaseID)
	case strings.TrimSpace(releaseFamilyKey) != "":
		title, files, err = loadNZBExportBinaries(ctx, store, "release_family_key", releaseFamilyKey, 0)
	case binaryID > 0:
		title, files, err = loadNZBExportBinaries(ctx, store, "id", "", binaryID)
	}
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no files found for export")
	}

	model := buildNZBModel(title, files)
	if err := writeNZB(model, outPath); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "exported files=%d segments=%d title=%q\n", len(files), countSegments(files), title)

	if probeYEnc {
		if err := probeNZBYEncPrefixes(ctx, cfg, serverID, files, prefixBytes); err != nil {
			return err
		}
	}
	return nil
}

func loadNZBExportRelease(ctx context.Context, store *pgindex.Store, releaseID string) (string, []nzbExportFile, error) {
	rel, err := store.GetCatalogReleaseByID(ctx, releaseID)
	if err != nil {
		return "", nil, err
	}
	if rel == nil {
		return "", nil, fmt.Errorf("release %q not found", releaseID)
	}
	groups, err := store.ListCatalogReleaseNewsgroups(ctx, releaseID)
	if err != nil {
		return "", nil, err
	}
	releaseFiles, err := store.ListCatalogReleaseFiles(ctx, releaseID)
	if err != nil {
		return "", nil, err
	}

	files := make([]nzbExportFile, 0, len(releaseFiles))
	for _, file := range releaseFiles {
		articles, err := store.ListCatalogReleaseFileArticles(ctx, file.ID)
		if err != nil {
			return "", nil, err
		}
		files = append(files, exportFileFromCatalog(file, groups, articles))
	}
	return rel.Title, files, nil
}

func exportFileFromCatalog(file pgindex.CatalogReleaseFile, groups []string, articles []pgindex.CatalogArticleRef) nzbExportFile {
	segments := make([]nzb.Segment, 0, len(articles))
	for _, article := range articles {
		segments = append(segments, nzb.Segment{
			Number:    article.PartNumber,
			Bytes:     article.Bytes,
			MessageID: trimMessageIDBrackets(article.MessageID),
		})
	}
	dateUnix := int64(0)
	if file.PostedAt != nil {
		dateUnix = file.PostedAt.Unix()
	}
	return nzbExportFile{
		ID:        file.ID,
		BinaryID:  file.BinaryID,
		FileName:  file.FileName,
		Subject:   firstNonEmpty(file.Subject, file.FileName),
		Poster:    file.Poster,
		DateUnix:  dateUnix,
		Groups:    groups,
		SizeBytes: file.SizeBytes,
		Index:     file.FileIndex,
		Segments:  segments,
	}
}

func loadNZBExportBinaries(ctx context.Context, store *pgindex.Store, mode, releaseFamilyKey string, binaryID int64) (string, []nzbExportFile, error) {
	var (
		rows *sql.Rows
		err  error
	)
	db := store.DB()
	switch mode {
	case "release_family_key":
		rows, err = db.QueryContext(ctx, `
			SELECT
				b.id,
				COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), 'binary-' || b.id::text) AS file_name,
				COALESCE(b.binary_name, b.file_name, '') AS subject,
				COALESCE(po.poster_name, '') AS poster,
				b.posted_at,
				COALESCE(b.total_bytes, 0),
				COALESCE(b.file_index, 0),
				ng.group_name
			FROM binaries b
			JOIN newsgroups ng ON ng.id = b.newsgroup_id
			LEFT JOIN posters po ON po.id = b.poster_id
			WHERE b.release_family_key = $1
			ORDER BY b.file_index, b.id`, releaseFamilyKey)
	case "id":
		rows, err = db.QueryContext(ctx, `
			SELECT
				b.id,
				COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), 'binary-' || b.id::text) AS file_name,
				COALESCE(b.binary_name, b.file_name, '') AS subject,
				COALESCE(po.poster_name, '') AS poster,
				b.posted_at,
				COALESCE(b.total_bytes, 0),
				COALESCE(b.file_index, 0),
				ng.group_name
			FROM binaries b
			JOIN newsgroups ng ON ng.id = b.newsgroup_id
			LEFT JOIN posters po ON po.id = b.poster_id
			WHERE b.id = $1
			ORDER BY b.file_index, b.id`, binaryID)
	default:
		return "", nil, fmt.Errorf("unknown export binary mode %q", mode)
	}
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()

	files := []nzbExportFile{}
	for rows.Next() {
		var (
			file     nzbExportFile
			postedAt sql.NullTime
			group    string
		)
		if err := rows.Scan(&file.BinaryID, &file.FileName, &file.Subject, &file.Poster, &postedAt, &file.SizeBytes, &file.Index, &group); err != nil {
			return "", nil, fmt.Errorf("scan export binary: %w", err)
		}
		if postedAt.Valid {
			file.DateUnix = postedAt.Time.Unix()
		}
		file.Groups = []string{group}
		segments, err := loadBinarySegments(ctx, store, file.BinaryID)
		if err != nil {
			return "", nil, err
		}
		file.Segments = segments
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return "", nil, fmt.Errorf("iterate export binaries: %w", err)
	}
	return strings.TrimSpace(firstNonEmpty(releaseFamilyKey, fmt.Sprintf("binary-%d", binaryID))), files, nil
}

func loadBinarySegments(ctx context.Context, store *pgindex.Store, binaryID int64) ([]nzb.Segment, error) {
	articles, err := store.ListBinaryPartArticles(ctx, binaryID)
	if err != nil {
		return nil, err
	}
	if len(articles) == 0 {
		return nil, nil
	}

	rows, err := store.DB().QueryContext(ctx, `
		SELECT
			ah.message_id,
			ah.bytes,
			bp.part_number
		FROM binary_parts bp
		JOIN article_headers ah ON ah.id = bp.article_header_id
		WHERE bp.binary_id = $1
		ORDER BY bp.part_number`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list binary %d segments: %w", binaryID, err)
	}
	defer rows.Close()

	segments := make([]nzb.Segment, 0, len(articles))
	for rows.Next() {
		var segment nzb.Segment
		if err := rows.Scan(&segment.MessageID, &segment.Bytes, &segment.Number); err != nil {
			return nil, fmt.Errorf("scan binary %d segment: %w", binaryID, err)
		}
		segment.MessageID = trimMessageIDBrackets(segment.MessageID)
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate binary %d segments: %w", binaryID, err)
	}
	return segments, nil
}

func buildNZBModel(title string, files []nzbExportFile) nzb.Model {
	model := nzb.Model{
		Meta: []nzb.Meta{
			{Type: "title", Content: strings.TrimSpace(title)},
			{Type: "generator", Content: "gonzb articleprobe"},
		},
		Files: make([]nzb.File, 0, len(files)),
	}
	for _, file := range files {
		groups := make([]string, 0, len(file.Groups))
		for _, group := range file.Groups {
			group = strings.TrimSpace(group)
			if group == "" {
				continue
			}
			groups = append(groups, group)
		}
		subject := strings.TrimSpace(file.Subject)
		if subject == "" {
			subject = strings.TrimSpace(file.FileName)
		}
		model.Files = append(model.Files, nzb.File{
			Subject:  subject,
			Poster:   strings.TrimSpace(file.Poster),
			Date:     file.DateUnix,
			Groups:   groups,
			Segments: file.Segments,
		})
	}
	return model
}

func writeNZB(model nzb.Model, outPath string) error {
	data, err := xml.MarshalIndent(model, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal nzb: %w", err)
	}
	data = append([]byte(xml.Header), data...)
	data = append(data, '\n')
	outPath = strings.TrimSpace(outPath)
	if outPath == "" || outPath == "-" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return fmt.Errorf("write nzb %s: %w", outPath, err)
	}
	fmt.Fprintf(os.Stderr, "wrote nzb: %s\n", outPath)
	return nil
}

func probeNZBYEncPrefixes(ctx context.Context, cfg *config.Config, serverID string, files []nzbExportFile, prefixBytes int64) error {
	server, err := chooseServer(cfg, serverID)
	if err != nil {
		return err
	}
	if prefixBytes <= 0 {
		prefixBytes = 8192
	}
	for _, file := range files {
		fmt.Fprintf(os.Stderr, "\n== file binary_id=%d index=%d name=%q segments=%d ==\n", file.BinaryID, file.Index, file.FileName, len(file.Segments))
		for _, segment := range file.Segments {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			prefix, err := fetchBodyPrefix(server, segment.MessageID, file.Groups, prefixBytes)
			if err != nil {
				fmt.Fprintf(os.Stderr, "part=%d msg=%s error=%v\n", segment.Number, segment.MessageID, err)
				continue
			}
			header, err := nzb.ReadYencHeader(bytes.NewReader(prefix))
			if err != nil {
				fmt.Fprintf(os.Stderr, "part=%d msg=%s yenc_header_error=%v\n", segment.Number, segment.MessageID, err)
				continue
			}
			fmt.Fprintf(os.Stderr, "part=%d msg=%s yenc_name=%q yenc_part=%d/%d file_size=%d offset=%d end=%d\n",
				segment.Number,
				segment.MessageID,
				header.FileName,
				header.PartNumber,
				header.TotalParts,
				header.FileSize,
				header.PartOffset,
				header.PartEnd,
			)
		}
	}
	return nil
}

func fetchBodyPrefix(server config.ServerConfig, messageID string, groups []string, maxBytes int64) ([]byte, error) {
	conn, err := dialServer(server)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	for _, group := range groups {
		if strings.TrimSpace(group) == "" {
			continue
		}
		if _, err := selectGroup(conn, group); err != nil {
			continue
		}
		break
	}

	ref, err := normalizeArticleRef(messageID, 0)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Cmd("BODY %s", formatArticleRef(ref)); err != nil {
		return nil, err
	}
	code, msg, err := conn.ReadCodeLine(222)
	if err != nil {
		return nil, fmt.Errorf("BODY failed (code %d): %s", code, msg)
	}
	return io.ReadAll(io.LimitReader(conn.DotReader(), maxBytes))
}

func countSegments(files []nzbExportFile) int {
	total := 0
	for _, file := range files {
		total += len(file.Segments)
	}
	return total
}

func trimMessageIDBrackets(messageID string) string {
	messageID = strings.TrimSpace(messageID)
	messageID = strings.TrimPrefix(messageID, "<")
	messageID = strings.TrimSuffix(messageID, ">")
	return messageID
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func withRuntimeServers(cfg *config.Config) (*config.Config, error) {
	if cfg == nil || strings.TrimSpace(cfg.Store.SQLitePath) == "" {
		return cfg, nil
	}
	store, err := settingsstore.NewStore(cfg.Store.SQLitePath)
	if err != nil {
		return nil, fmt.Errorf("open runtime settings: %w", err)
	}
	defer store.Close()

	runtime, err := store.GetRuntimeSettings(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("load runtime settings: %w", err)
	}

	effective := settingsstore.ApplyToConfig(cfg, runtime)
	if effective == nil {
		return nil, fmt.Errorf("effective config is nil")
	}

	// articleprobe is an indexer-side BODY/HEAD tool; prefer the indexer NNTP pool
	// even when the compatibility/shared server list is present but incomplete.
	if servers := app.IndexerNNTPServers(runtime); len(servers) > 0 {
		effective.Servers = app.ToConfigServers(servers)
	}

	if err := effective.ValidateEffective(); err != nil {
		return nil, fmt.Errorf("validate effective config: %w", err)
	}
	return effective, nil
}

func fatalIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func normalizeArticleRef(messageID string, articleNumber int64) (articleRef, error) {
	ref := articleRef{
		messageID:     strings.TrimSpace(messageID),
		articleNumber: articleNumber,
	}
	if ref.messageID == "" && ref.articleNumber <= 0 {
		return articleRef{}, fmt.Errorf("either --message-id or --article-number must be provided")
	}
	if ref.messageID != "" && !strings.HasPrefix(ref.messageID, "<") {
		ref.messageID = "<" + ref.messageID + ">"
	}
	return ref, nil
}

func resolveArticleProbeServerID(values ...string) (string, error) {
	selected := ""
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if selected == "" {
			selected = value
			continue
		}
		if selected != value {
			return "", fmt.Errorf("--server, --nntp-provider-id, and --provider-id must not specify different values")
		}
	}
	return selected, nil
}

func chooseServer(cfg *config.Config, serverID string) (config.ServerConfig, error) {
	if cfg == nil {
		return config.ServerConfig{}, fmt.Errorf("config is required")
	}
	if len(cfg.Servers) == 0 {
		return config.ServerConfig{}, fmt.Errorf("no servers configured")
	}
	if strings.TrimSpace(serverID) == "" {
		return cfg.Servers[0], nil
	}
	for _, server := range cfg.Servers {
		if server.ID == serverID {
			return server, nil
		}
	}
	return config.ServerConfig{}, fmt.Errorf("server %q not found in config", serverID)
}

func dialServer(server config.ServerConfig) (*textproto.Conn, error) {
	addr := net.JoinHostPort(server.Host, strconv.Itoa(server.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}

	var (
		netConn net.Conn
		err     error
	)
	if server.TLS {
		netConn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
			ServerName: server.Host,
			MinVersion: tls.VersionTLS12,
		})
	} else {
		netConn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	conn := textproto.NewConn(netConn)
	code, msg, err := conn.ReadCodeLine(200)
	if tpErr, ok := err.(*textproto.Error); ok && tpErr.Code == 201 {
		err = nil
		code = tpErr.Code
		msg = tpErr.Msg
	}
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("nntp greeting failed (code %d): %s", code, msg)
	}

	if server.Username != "" {
		if _, err := conn.Cmd("AUTHINFO USER %s", server.Username); err != nil {
			conn.Close()
			return nil, fmt.Errorf("auth user: %w", err)
		}
		if _, _, err := conn.ReadCodeLine(381); err != nil {
			conn.Close()
			return nil, fmt.Errorf("auth user rejected: %w", err)
		}
		if _, err := conn.Cmd("AUTHINFO PASS %s", server.Password); err != nil {
			conn.Close()
			return nil, fmt.Errorf("auth pass: %w", err)
		}
		if _, _, err := conn.ReadCodeLine(281); err != nil {
			conn.Close()
			return nil, fmt.Errorf("auth pass rejected: %w", err)
		}
	}

	return conn, nil
}

type groupStats struct {
	group string
	count int64
	low   int64
	high  int64
}

func selectGroup(conn *textproto.Conn, group string) (groupStats, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return groupStats{}, fmt.Errorf("group is required")
	}
	if _, err := conn.Cmd("GROUP %s", group); err != nil {
		return groupStats{}, err
	}
	code, msg, err := conn.ReadCodeLine(211)
	if err != nil {
		return groupStats{}, fmt.Errorf("GROUP %s failed (code %d): %s", group, code, msg)
	}
	parts := strings.Fields(msg)
	if len(parts) < 4 {
		return groupStats{}, fmt.Errorf("unexpected GROUP response: %q", msg)
	}
	count, _ := strconv.ParseInt(parts[0], 10, 64)
	low, _ := strconv.ParseInt(parts[1], 10, 64)
	high, _ := strconv.ParseInt(parts[2], 10, 64)
	return groupStats{group: group, count: count, low: low, high: high}, nil
}

func printStat(conn *textproto.Conn, ref articleRef) error {
	label := formatArticleRef(ref)
	if _, err := conn.Cmd("STAT %s", label); err != nil {
		return err
	}
	code, msg, err := conn.ReadCodeLine(223)
	if err != nil {
		return fmt.Errorf("STAT failed (code %d): %s", code, msg)
	}
	fmt.Printf("\n== STAT ==\n%s\n", msg)
	return nil
}

func printXOver(conn *textproto.Conn, articleNumber int64) error {
	if articleNumber <= 0 {
		return nil
	}
	lines, err := fetchXOverLines(conn, articleNumber, articleNumber)
	if err != nil {
		return err
	}
	fmt.Printf("\n== XOVER ==\n")
	if len(lines) == 0 {
		fmt.Println("(no rows)")
		return nil
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	return nil
}

func fetchXOverLines(conn *textproto.Conn, start, end int64) ([]string, error) {
	if start <= 0 || end <= 0 {
		return nil, fmt.Errorf("xover range must be positive")
	}
	if end < start {
		return nil, fmt.Errorf("xover end must be greater than or equal to start")
	}
	if _, err := conn.Cmd("XOVER %d-%d", start, end); err != nil {
		return nil, err
	}
	code, msg, err := conn.ReadCodeLine(224)
	if err != nil {
		return nil, fmt.Errorf("XOVER failed (code %d): %s", code, msg)
	}
	lines, err := readDotLines(conn)
	if err != nil {
		return nil, err
	}
	return lines, nil
}

func writeOutputFile(path string, data []byte) error {
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write output %s: %w", path, err)
	}
	return nil
}

func printHead(conn *textproto.Conn, ref articleRef, maxLines int) error {
	if _, err := conn.Cmd("HEAD %s", formatArticleRef(ref)); err != nil {
		return err
	}
	code, msg, err := conn.ReadCodeLine(221)
	if err != nil {
		return fmt.Errorf("HEAD failed (code %d): %s", code, msg)
	}
	lines, err := readDotLines(conn)
	if err != nil {
		return err
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	fmt.Printf("\n== HEAD ==\n")
	for _, line := range lines {
		fmt.Println(line)
	}
	return nil
}

func printBody(conn *textproto.Conn, ref articleRef, maxBytes int64) error {
	if _, err := conn.Cmd("BODY %s", formatArticleRef(ref)); err != nil {
		return err
	}
	code, msg, err := conn.ReadCodeLine(222)
	if err != nil {
		return fmt.Errorf("BODY failed (code %d): %s", code, msg)
	}
	text, truncated, err := readDotTextLimited(conn, maxBytes)
	if err != nil {
		return err
	}
	fmt.Printf("\n== BODY ==\n%s", text)
	if truncated {
		fmt.Printf("\n... [truncated after %d bytes]\n", maxBytes)
	}
	return nil
}

func printArticle(conn *textproto.Conn, ref articleRef, maxBytes int64) error {
	if _, err := conn.Cmd("ARTICLE %s", formatArticleRef(ref)); err != nil {
		return err
	}
	code, msg, err := conn.ReadCodeLine(220)
	if err != nil {
		return fmt.Errorf("ARTICLE failed (code %d): %s", code, msg)
	}
	text, truncated, err := readDotTextLimited(conn, maxBytes)
	if err != nil {
		return err
	}
	fmt.Printf("\n== ARTICLE ==\n%s", text)
	if truncated {
		fmt.Printf("\n... [truncated after %d bytes]\n", maxBytes)
	}
	return nil
}

func readDotLines(conn *textproto.Conn) ([]string, error) {
	reader := bufio.NewScanner(conn.DotReader())
	reader.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lines := []string{}
	for reader.Scan() {
		lines = append(lines, reader.Text())
	}
	if err := reader.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func readDotTextLimited(conn *textproto.Conn, maxBytes int64) (string, bool, error) {
	data, err := io.ReadAll(conn.DotReader())
	if err != nil {
		return "", false, err
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return string(data[:maxBytes]), true, nil
	}
	return string(data), false, nil
}

func formatArticleRef(ref articleRef) string {
	if ref.messageID != "" {
		return ref.messageID
	}
	return strconv.FormatInt(ref.articleNumber, 10)
}

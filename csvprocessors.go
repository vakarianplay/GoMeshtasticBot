package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type NeighbourRow struct {
	NodeNum   uint32
	UserID    string
	ShortName string
	LongName  string
	LastHeard int64
}

func SaveAllNeighboursToCSV(neighboursNodes string, filePath string) error {
	rows := parseAllNeighbours(neighboursNodes)

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].NodeNum < rows[j].NodeNum
	})

	// Перезапись файла при каждом вызове
	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create csv: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)

	if err := w.Write([]string{
		"node_num",
		"user_id",
		"short_name",
		"long_name",
		"last_heard_unix",
		"last_heard_rfc3339",
	}); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	for _, r := range rows {
		lastUnix := ""
		lastRFC := ""
		if r.LastHeard > 0 {
			lastUnix = strconv.FormatInt(r.LastHeard, 10)
			lastRFC = time.Unix(r.LastHeard, 0).UTC().Format(time.RFC3339)
		}

		if err := w.Write([]string{
			strconv.FormatUint(uint64(r.NodeNum), 10),
			r.UserID,
			r.ShortName,
			r.LongName,
			lastUnix,
			lastRFC,
		}); err != nil {
			return fmt.Errorf("write row: %w", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("flush csv: %w", err)
	}

	return nil
}

func parseAllNeighbours(s string) []NeighbourRow {
	parts := strings.Split(s, "num:")
	if len(parts) <= 1 {
		return nil
	}

	// Регекспы только на отдельные поля (без lookahead)
	numRe := regexp.MustCompile(`^\s*(\d+)`)
	idRe := regexp.MustCompile(`\bid:"([^"]*)"`)
	shortRe := regexp.MustCompile(`\bshort_name:"([^"]*)"`)
	longRe := regexp.MustCompile(`\blong_name:"([^"]*)"`)
	lastHeardRe := regexp.MustCompile(`\blast_heard:(\d+)`)

	out := make([]NeighbourRow, 0, len(parts)-1)

	// parts[0] — мусор до первого num:, пропускаем
	for i := 1; i < len(parts); i++ {
		block := strings.TrimSpace(parts[i])
		if block == "" {
			continue
		}

		var row NeighbourRow

		if m := numRe.FindStringSubmatch(block); len(m) == 2 {
			if v, err := strconv.ParseUint(m[1], 10, 32); err == nil {
				row.NodeNum = uint32(v)
			}
		}

		if m := idRe.FindStringSubmatch(block); len(m) == 2 {
			row.UserID = strings.TrimSpace(m[1])
		}
		if m := shortRe.FindStringSubmatch(block); len(m) == 2 {
			row.ShortName = strings.TrimSpace(m[1])
		}
		if m := longRe.FindStringSubmatch(block); len(m) == 2 {
			row.LongName = strings.TrimSpace(m[1])
		}
		if m := lastHeardRe.FindStringSubmatch(block); len(m) == 2 {
			if v, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				row.LastHeard = v
			}
		}

		if row.NodeNum != 0 {
			out = append(out, row)
		}
	}

	return out
}

func GetNames(filePath string, nodeID string) (string, string, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return "", "", fmt.Errorf("empty node_id")
	}

	f, err := os.Open(filePath)
	if err != nil {
		return "", "", fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)

	// читаем заголовок
	header, err := r.Read()
	if err != nil {
		if err == io.EOF {
			return "", "", fmt.Errorf("csv is empty")
		}
		return "", "", fmt.Errorf("read header: %w", err)
	}

	col := make(map[string]int, len(header))
	for i, h := range header {
		col[strings.TrimSpace(strings.ToLower(h))] = i
	}

	req := []string{"node_num", "user_id", "short_name", "long_name"}
	for _, k := range req {
		if _, ok := col[k]; !ok {
			return "", "", fmt.Errorf("missing column %q", k)
		}
	}

	for {
		rec, err := r.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", "", fmt.Errorf("read row: %w", err)
		}

		// защита от коротких строк
		maxIdx := 0
		for _, idx := range []int{
			col["node_num"],
			col["user_id"],
			col["short_name"],
			col["long_name"],
		} {
			if idx > maxIdx {
				maxIdx = idx
			}
		}
		if len(rec) <= maxIdx {
			continue
		}

		nodeNum := strings.TrimSpace(rec[col["node_num"]])
		userID := strings.TrimSpace(rec[col["user_id"]])

		if nodeID == nodeNum || strings.EqualFold(nodeID, userID) {
			shortName := strings.TrimSpace(rec[col["short_name"]])
			longName := strings.TrimSpace(rec[col["long_name"]])
			return shortName, longName, nil
		}
	}

	return "", "", fmt.Errorf("node_id %q not found", nodeID)
}

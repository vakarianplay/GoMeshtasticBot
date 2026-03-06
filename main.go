package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/lmatte7/gomesh"
	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
)

const (
	nodeAddress = "192.168.88.49"
	csvPath     = "heard_nodes_history.csv"
)

func main() {
	var radio gomesh.Radio

	if err := radio.Init(nodeAddress); err != nil {
		log.Fatal(err)
	}
	defer radio.Close()

	nodeInfo(radio)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	log.Printf("Start appending heard nodes history to %s. Delay update: %s", csvPath, time.Minute*10)
	if err := runHeardNodesHistoryUpdater(ctx, &radio, csvPath, time.Minute*10); err != nil {
		log.Fatal(err)
	}
}

func nodeInfo(radio gomesh.Radio) {
	responses, err := radio.GetRadioInfo()
	if err != nil {
		log.Fatal(err)
	}

	var myNum uint32
	var myInfo *pb.FromRadio_MyInfo
	var myNode *pb.NodeInfo

	for _, r := range responses {
		if info, ok := r.GetPayloadVariant().(*pb.FromRadio_MyInfo); ok {
			myInfo = info
			myNum = info.MyInfo.MyNodeNum
		}
		if ni, ok := r.GetPayloadVariant().(*pb.FromRadio_NodeInfo); ok {
			if myNum != 0 && ni.NodeInfo.Num == myNum {
				myNode = ni.NodeInfo
			}
		}
	}

	var metrics string
	var name string
	var userId string
	if myNode != nil && myNode.User != nil && myNode.User.LongName != "" {
		name = myNode.User.LongName
		userId = myNode.User.Id
		metrics = myNode.DeviceMetrics.String()
	} else if myNode != nil && myNode.User != nil {
		name = myNode.User.ShortName

	}

	fmt.Println("Node num: ", myNum)
	fmt.Println("ID: ", userId)
	fmt.Println("Name: ", name)
	fmt.Println("Metrics: ", metrics)
	fmt.Println("Node info: ", myInfo.MyInfo.String())
}

func runHeardNodesHistoryUpdater(
	ctx context.Context,
	radio *gomesh.Radio,
	filePath string,
	interval time.Duration,
) error {
	knownNodes := make(map[uint32]*pb.NodeInfo)

	// Первый снимок сразу
	if err := appendHeardNodesSnapshotCSV(radio, filePath, knownNodes); err != nil {
		log.Printf("append snapshot error: %v", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Updater stopped")
			return nil
		case <-ticker.C:
			if err := appendHeardNodesSnapshotCSV(radio, filePath, knownNodes); err != nil {
				log.Printf("append snapshot error: %v", err)
			}
		}
	}
}

func appendHeardNodesSnapshotCSV(
	radio *gomesh.Radio,
	filePath string,
	knownNodes map[uint32]*pb.NodeInfo,
) error {
	responses, err := radio.GetRadioInfo()
	if err != nil {
		return fmt.Errorf("GetRadioInfo: %w", err)
	}

	// Текущий снимок
	current := make(map[uint32]*pb.NodeInfo)
	for _, r := range responses {
		if ni, ok := r.GetPayloadVariant().(*pb.FromRadio_NodeInfo); ok {
			n := ni.NodeInfo
			if n == nil {
				continue
			}
			current[n.GetNum()] = n
		}
	}

	// Обновляем кэш известных узлов свежими данными
	for num, n := range current {
		knownNodes[num] = n
	}

	// Пишем заголовок, если файл пустой
	if err := ensureCSVHeader(filePath); err != nil {
		return err
	}

	// Пишем все knownNodes; для отсутствующих в current seen=0
	nums := make([]uint32, 0, len(knownNodes))
	for num := range knownNodes {
		nums = append(nums, num)
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })

	snapshotAt := time.Now().Format(time.DateTime)

	for _, num := range nums {
		n := knownNodes[num]
		if n == nil {
			continue
		}

		seen := "0"
		if _, ok := current[num]; ok {
			seen = "1"
		}

		var userID, longName, shortName string
		if u := n.GetUser(); u != nil {
			userID = u.GetId()
			longName = u.GetLongName()
			shortName = u.GetShortName()
		}

		lastHeardUnix := n.GetLastHeard()
		lastHeardTime := ""
		if lastHeardUnix > 0 {
			lastHeardTime = time.Unix(int64(lastHeardUnix), 0).Format(time.DateTime)
		}

		row, err := formatCSVRow([]string{
			snapshotAt,
			fmt.Sprintf("%d", n.GetNum()),
			userID,
			longName,
			shortName,
			fmt.Sprintf("%d", lastHeardUnix),
			lastHeardTime,
			fmt.Sprintf("%d", n.GetChannel()),
			seen,
		})
		if err != nil {
			return fmt.Errorf("format csv row: %w", err)
		}

		if err := csvWriter(filePath, row); err != nil {
			return fmt.Errorf("csvWriter: %w", err)
		}
	}

	return nil
}

func ensureCSVHeader(filePath string) error {
	info, err := os.Stat(filePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat csv: %w", err)
	}

	if err == nil && info.Size() > 0 {
		return nil
	}

	header, err := formatCSVRow([]string{
		"snapshot_at",
		"node_num",
		"user_id",
		"long_name",
		"short_name",
		"last_heard_unix",
		"last_heard_time",
		"channel",
		"seen",
	})
	if err != nil {
		return fmt.Errorf("format header: %w", err)
	}

	if err := csvWriter(filePath, header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	return nil
}

// formatCSVRow корректно экранирует поля CSV и возвращает готовую строку.
func formatCSVRow(fields []string) (string, error) {
	var b strings.Builder
	w := csv.NewWriter(&b)

	if err := w.Write(fields); err != nil {
		return "", err
	}
	w.Flush()

	if err := w.Error(); err != nil {
		return "", err
	}

	return b.String(), nil
}

// csvWriter пишет одну готовую CSV-строку в конец файла.
func csvWriter(filePath string, row string) error {
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	if !strings.HasSuffix(row, "\n") {
		row += "\n"
	}

	if _, err := f.WriteString(row); err != nil {
		return fmt.Errorf("write csv row: %w", err)
	}

	return nil
}

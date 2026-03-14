package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lmatte7/gomesh"
	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
)

func main() {
	var radio gomesh.Radio
	var myNodeNum uint32

	if err := radio.Init("192.168.88.45"); err != nil {
		log.Fatalf("init error: %v", err)
	}
	defer radio.Close()

	nodeInfo(radio)
	time.Sleep(200 * time.Millisecond)

	if err := SaveAllNeighboursToCSV(getNeighbours(radio), "nodes.csv"); err != nil {
		fmt.Println("save error:", err)
	} else {
		log.Println("Neighbours list update")
	}

	log.Println("=====B O T    S T A R T E D======\n")
	for {
		packets, err := radio.ReadResponse(true)
		if err != nil {
			log.Printf("read error: %v", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		for _, fr := range packets {
			switch v := fr.GetPayloadVariant().(type) {
			case *pb.FromRadio_MyInfo:
				myNodeNum = v.MyInfo.GetMyNodeNum()
				log.Printf("[MYINFO] myNodeNum=%d", myNodeNum)

			case *pb.FromRadio_Packet:
				mp := v.Packet
				if mp == nil {
					continue
				}
				decoded := mp.GetDecoded()
				if decoded == nil {
					continue
				}
				if decoded.GetPortnum() != pb.PortNum_TEXT_MESSAGE_APP {
					continue
				}

				text := strings.TrimSpace(string(decoded.GetPayload()))
				from := mp.GetFrom()
				to := mp.GetTo()
				ch := mp.GetChannel()
				shortName, fullName, _ := GetNames("nodes.csv", fmt.Sprint(from))

				log.Printf("[TEXT] my=%d from=%d to=%d (0x%08X) ch=%d id=%d  %s %s: %s", myNodeNum, from, to, to, ch, mp.GetId(), shortName, fullName, text)
				info := fmt.Sprintf("HOPS=%d   RSSI=%d dBm    SNR=%.1f dB", mp.GetHopStart()-mp.GetHopLimit(), mp.GetRxRssi(), mp.GetRxSnr())
				msgRelayer(text, shortName, fullName, fmt.Sprint(from), info)

				// Не отвечаем сами себе (только если уже знаем myNodeNum)
				if myNodeNum != 0 && from == myNodeNum {
					continue
				}

				var reply string
				switch text {
				case "/ping":
					reply = buildPingReply(mp)
				case "/info":
					ansStr, err := getInfoSting()
					if err != nil {
						log.Printf("getInfoSting error: %v", err)
						reply = "Ошибка получения погоды"
					} else {
						reply = ansStr
					}
				case "/about":
					reply = "Meshtastic бот на golang. Разработка: https://vakarian.website \n Репозиторий: https://github.com/vakarianplay/GoMeshtasticBot"

				default:
					continue
				}

				// Определяем тип входящего:
				// broadcast может быть 0 или 0xFFFFFFFF
				isBroadcast := to == 0 || to == ^uint32(0)
				isLongFast := ch == 0

				// Если myNodeNum пока 0, считаем любой non-broadcast как DM (fallback)
				isDM := !isBroadcast && (myNodeNum == 0 || to == myNodeNum)
				isPublicLongFast := isBroadcast && isLongFast

				log.Printf(
					"route: my=%d isDM=%v isBroadcast=%v isLongFast=%v isPublicLongFast=%v",
					myNodeNum, isDM, isBroadcast, isLongFast, isPublicLongFast,
				)

				switch {
				case isDM:
					// Ответ в личку отправителю
					if err := radio.SendTextMessage(reply, int64(from), 0); err != nil {
						log.Printf("send DM error: %v", err)
					} else {
						log.Printf("DM reply sent to %d", from)
					}

				case isPublicLongFast:
					// Ответ в публичный LongFast (channel 0)
					if err := radio.SendTextMessage(reply, 0, int64(ch)); err != nil {
						log.Printf("send public error: %v", err)
					} else {
						log.Printf("public reply sent to channel=%d", ch)
					}

				default:
					log.Printf("skip: unknown route from=%d to=%d ch=%d", from, to, ch)
				}
			}
		}

		time.Sleep(200 * time.Millisecond)
	}
}

func buildPingReply(mp *pb.MeshPacket) string {
	rssi := mp.GetRxRssi() // dBm
	snr := mp.GetRxSnr()   // dB

	// hops = hop_start - hop_limit
	hopStart := int64(mp.GetHopStart())
	hopLimit := int64(mp.GetHopLimit())

	var hops int64
	if hopStart > 0 && hopStart >= hopLimit {
		hops = hopStart - hopLimit
	} else {
		hops = 0
	}

	return fmt.Sprintf(
		"pong | RSSI: %d dBm | SNR: %.1f dB | hops: %d",
		rssi, snr, hops,
	)
}

func getInfoSting() (string, error) {
	apiKey := "28bc310f78d0674674d5ca06e7a2a556"
	if apiKey == "" {
		return "", fmt.Errorf("empty apiKey")
	}

	url := fmt.Sprintf(
		"https://api.openweathermap.org/data/2.5/weather?q=Elektrostal,RU&units=metric&lang=ru&appid=%s",
		apiKey,
	)

	c := &http.Client{Timeout: 6 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf(
			"owm status %d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(b)),
		)
	}

	var r struct {
		Dt       int64  `json:"dt"`
		Timezone int64  `json:"timezone"`
		Name     string `json:"name"`
		Main     struct {
			Temp     float64 `json:"temp"`
			Humidity int     `json:"humidity"`
		} `json:"main"`
		Weather []struct {
			Description string `json:"description"`
		} `json:"weather"`
		Sys struct {
			Sunrise int64 `json:"sunrise"`
			Sunset  int64 `json:"sunset"`
		} `json:"sys"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}

	locTime := func(ts int64) string {
		return time.Unix(ts+r.Timezone, 0).UTC().Format("2006-01-02 15:04")
	}

	desc := ""
	if len(r.Weather) > 0 {
		desc = r.Weather[0].Description
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	return fmt.Sprintf(
		"Сейчас: %s | %s: %.1f°C, влажн. %d%%, %s | Рассвет: %s | Закат: %s",
		now, r.Name, r.Main.Temp, r.Main.Humidity, desc,
		locTime(r.Sys.Sunrise), locTime(r.Sys.Sunset),
	), nil
}

func nodeInfo(radio gomesh.Radio) {
	responses, err := radio.GetRadioInfo()
	if err != nil {
		log.Fatal(err)
	}

	var myNum uint32
	var myInfo *pb.FromRadio_MyInfo
	var myNode *pb.NodeInfo

	var neighbourd string

	for _, r := range responses {
		if info, ok := r.GetPayloadVariant().(*pb.FromRadio_MyInfo); ok {
			myInfo = info
			myNum = info.MyInfo.MyNodeNum
		}
		if ni, ok := r.GetPayloadVariant().(*pb.FromRadio_NodeInfo); ok {
			neighbourd = neighbourd + (fmt.Sprint(ni.NodeInfo))
			// log.Println(ni.NodeInfo)
			if myNum != 0 && ni.NodeInfo.Num == myNum {
				myNode = ni.NodeInfo
			}
		}
	}

	var metrics string
	var name string
	var userId string
	var model string
	if myNode != nil && myNode.User != nil && myNode.User.LongName != "" {
		name = myNode.User.LongName
		userId = myNode.User.Id
		metrics = myNode.DeviceMetrics.String()
		model = myNode.User.HwModel.String()
	} else if myNode != nil && myNode.User != nil {
		name = myNode.User.ShortName
	}

	fmt.Println("Node num: ", myNum)
	fmt.Println("ID: ", userId)
	fmt.Println("Name: ", name)
	fmt.Println("Hardware: ", model)
	fmt.Println("Metrics: ", metrics)
	if myInfo != nil && myInfo.MyInfo != nil {
		fmt.Println("Node info: ", myInfo.MyInfo.String())
	}
}

func getNeighbours(radio gomesh.Radio) string {
	var result string
	responses, err := radio.GetRadioInfo()
	if err != nil {
		log.Fatal(err)
	}
	for _, r := range responses {
		if ni, ok := r.GetPayloadVariant().(*pb.FromRadio_NodeInfo); ok {
			result = result + (fmt.Sprint(ni.NodeInfo))
			// log.Println(ni.NodeInfo)
		}
	}
	return result
}

func msgRelayer(message, shortName, fullName, nodeId, info string) {
	firstString := "(" + shortName + ")  " + fullName + " | id: " + nodeId
	secondString := message
	thirdString := info

	fmt.Println(firstString, "\n\n", secondString, "\n\n", thirdString)
}

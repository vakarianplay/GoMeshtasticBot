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

	log.Println("bot started")

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

				log.Printf(
					"[TEXT] my=%d from=%d to=%d (0x%08X) ch=%d id=%d: %s",
					myNodeNum, from, to, to, ch, mp.GetId(), text,
				)

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

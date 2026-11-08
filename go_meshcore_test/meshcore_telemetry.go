// meshcore-telemetry — запрос телеметрии с узла MeshCore через TCP-интерфейс
// компаньона (WiFi-сборка прошивки).
//
// Сборка:  go build -o meshcore-telemetry .
// Запуск:  ./meshcore-telemetry "My-Repeater"
//
// Байтовые раскладки взяты из:
//   examples/companion_radio/MyMesh.cpp        (коды команд и ответов)
//   src/helpers/wifi/SerialWifiInterface.cpp   (обрамление TCP-кадров)
//   src/helpers/sensors/LPPDataHelpers.h       (Cayenne LPP)
package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"time"
)

// --- обрамление кадров (SerialWifiInterface.cpp) ---
// app -> radio: '<' + uint16 LE длина + payload
// radio -> app: '>' + uint16 LE длина + payload
const (
	frameOutMagic = '<'
frameInMagic  = '>'
maxFrame      = 512 // щедрый потолок; свой лимит прошивка проверяет сама
)

// --- команды (CMD_*) ---
const (
	cmdAppStart      = 1
	cmdGetContacts   = 4
	cmdSetDeviceTime = 6
	cmdDeviceQuery   = 22
	cmdSendTelemetry = 39 // 0x27
	appTargetVer     = 3
)

// --- ответы и пуши (RESP_CODE_* / PUSH_CODE_*) ---
const (
	respOK            = 0
	respErr           = 1
	respContactsStart = 2
	respContact       = 3
	respContactsEnd   = 4
	respSelfInfo      = 5
	respSent          = 6
	respDeviceInfo    = 13
	pushTelemetryResp = 0x8B
)

const advTypeRepeater = 2

var errTimeout = errors.New("таймаут ожидания ответа")

// ---------------------------------------------------------------------------
// Транспорт
// ---------------------------------------------------------------------------

type link struct {
	conn    net.Conn
	r       *bufio.Reader
	verbose bool
}

func dial(host string, port int) (*link, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("подключение к %s: %w", addr, err)
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}
	return &link{conn: conn, r: bufio.NewReader(conn)}, nil
}

func (l *link) writeFrame(payload []byte) error {
	if l.verbose {
		fmt.Fprintf(os.Stderr, ">> [%d] %s\n", len(payload), hex.EncodeToString(payload))
	}
	buf := make([]byte, 0, 3+len(payload))
	buf = append(buf, frameOutMagic, byte(len(payload)), byte(len(payload)>>8))
	buf = append(buf, payload...)
	_, err := l.conn.Write(buf)
	return err
}

// readFrame читает один кадр. При рассинхронизации ищет следующий '>'.
func (l *link) readFrame(deadline time.Time) ([]byte, error) {
	for {
		if err := l.conn.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
		magic, err := l.r.ReadByte()
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return nil, errTimeout
			}
			return nil, err
		}
		if magic != frameInMagic {
			continue
		}
		var lb [2]byte
		if _, err := io.ReadFull(l.r, lb[:]); err != nil {
			return nil, err
		}
		n := int(binary.LittleEndian.Uint16(lb[:]))
		if n == 0 || n > maxFrame {
			continue // битая длина — ресинхронизация
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(l.r, buf); err != nil {
			return nil, err
		}
		if l.verbose {
			fmt.Fprintf(os.Stderr, "<< [%d] %s\n", n, hex.EncodeToString(buf))
		}
		return buf, nil
	}
}

// waitFor возвращает первый кадр, чей код перечислен в want; прочие пропускает.
// Именно пропуск посторонних кадров позволяет пережить поток контактов и пуши.
func (l *link) waitFor(want map[byte]bool, deadline time.Time) ([]byte, error) {
	for {
		f, err := l.readFrame(deadline)
		if err != nil {
			return nil, err
		}
		if want[f[0]] {
			return f, nil
		}
		if l.verbose {
			fmt.Fprintf(os.Stderr, "   (пропущен кадр 0x%02x %s)\n", f[0], packetName(f[0]))
		}
	}
}

func packetName(b byte) string {
	switch b {
		case respOK:
			return "OK"
		case respErr:
			return "ERR"
		case respContactsStart:
			return "CONTACTS_START"
		case respContact:
			return "CONTACT"
		case respContactsEnd:
			return "CONTACTS_END"
		case respSelfInfo:
			return "SELF_INFO"
		case respSent:
			return "SENT"
		case respDeviceInfo:
			return "DEVICE_INFO"
		case 0x83:
			return "MSG_WAITING"
		case pushTelemetryResp:
			return "TELEMETRY_RESPONSE"
	}
	return fmt.Sprintf("code=%d", b)
}

func errCodeName(c byte) string {
	switch c {
		case 1:
			return "UNSUPPORTED_CMD"
		case 2:
			return "NOT_FOUND"
		case 3:
			return "TABLE_FULL"
		case 4:
			return "BAD_STATE"
		case 5:
			return "FILE_IO_ERROR"
		case 6:
			return "ILLEGAL_ARG"
	}
	return fmt.Sprintf("code=%d", c)
}

// ---------------------------------------------------------------------------
// Инициализация сессии
// ---------------------------------------------------------------------------

func (l *link) handshake(deadline time.Time) error {
	// CMD_APP_START: 1 + 7 зарезервированных байт + имя приложения.
	// Побочный эффект, который нам полезен: прошивка сбрасывает _iter_started,
	// поэтому GET_CONTACTS не вернёт BAD_STATE из-за чужого незакрытого списка.
	start := make([]byte, 8, 32)
	start[0] = cmdAppStart
	start = append(start, []byte("go-telemetry")...)
	if err := l.writeFrame(start); err != nil {
		return err
	}
	if _, err := l.waitFor(map[byte]bool{respSelfInfo: true, respErr: true}, deadline); err != nil {
		return fmt.Errorf("APP_START: %w", err)
	}

	// CMD_DEVICE_QUERY: сообщаем версию протокола (app_target_ver >= 3).
	_ = l.writeFrame([]byte{cmdDeviceQuery, appTargetVer})
	if _, err := l.waitFor(map[byte]bool{respDeviceInfo: true, respErr: true}, deadline); err != nil {
		return fmt.Errorf("DEVICE_QUERY: %w", err)
	}

	// CMD_SET_DEVICE_TIME: прошивка принимает только время "вперёд", ошибку игнорируем.
	ts := make([]byte, 5)
	ts[0] = cmdSetDeviceTime
	binary.LittleEndian.PutUint32(ts[1:], uint32(time.Now().Unix()))
	_ = l.writeFrame(ts)
	_, _ = l.waitFor(map[byte]bool{respOK: true, respErr: true}, deadline)
	return nil
}

// ---------------------------------------------------------------------------
// Список контактов
// ---------------------------------------------------------------------------

type contact struct {
	Name   string
	PubKey []byte // 32 байта
	Type   byte
}

// parseContact разбирает кадр RESP_CODE_CONTACT.
// Раскладка из writeContactRespFrame():
//
//	[0]     code
//	[1:33]  pub_key (32)                 <- фиксированный offset
//	[33]    type, [34] flags, [35] out_path_len
//	[36:36+MAX_PATH_SIZE] out_path       <- ЕДИНСТВЕННОЕ поле переменной длины
//	name(32) + last_advert(4) + lat(4) + lon(4) + lastmod(4)
//
// Хвост после имени всегда 16 байт, поэтому имя начинается с len-48 и знать
// MAX_PATH_SIZE не нужно.
func parseContact(f []byte) (contact, bool) {
	const tail = 32 + 16
	if len(f) < 1+32+3+tail {
		return contact{}, false
	}
	nameOff := len(f) - tail
	name := strings.TrimRight(string(f[nameOff:nameOff+32]), "\x00")
	return contact{
		Name:   name,
		PubKey: append([]byte(nil), f[1:33]...),
		Type:   f[33],
	}, true
}

// listContacts вычитывает поток контактов до RESP_CODE_END_OF_CONTACTS.
// Поток формируется в MyMesh::loop() по одному кадру за итерацию, поэтому
// чтение может занять секунды на большой книге контактов.
func listContacts(l *link, deadline time.Time) ([]contact, error) {
	if err := l.writeFrame([]byte{cmdGetContacts}); err != nil {
		return nil, err
	}
	if _, err := l.waitFor(map[byte]bool{respContactsStart: true, respErr: true}, deadline); err != nil {
		return nil, fmt.Errorf("GET_CONTACTS: %w", err)
	}

	want := map[byte]bool{respContact: true, respContactsEnd: true, respErr: true}
	var out []contact
	for {
		f, err := l.waitFor(want, deadline)
		if err != nil {
			return nil, err
		}
		switch f[0] {
			case respContactsEnd:
				return out, nil
			case respErr:
				code := byte(0)
				if len(f) > 1 {
					code = f[1]
				}
				return nil, fmt.Errorf("узел вернул ошибку %s", errCodeName(code))
			case respContact:
				if c, ok := parseContact(f); ok {
					out = append(out, c)
				}
		}
	}
}

// findContact ищет узел по имени: сначала точное совпадение, затем
// регистронезависимое. Список вычитывается полностью, чтобы не оставить
// итератор прошивки в активном состоянии.
func findContact(l *link, name string, deadline time.Time) (*contact, []string, error) {
	all, err := listContacts(l, deadline)
	if err != nil {
		return nil, nil, err
	}

	var fallback *contact
	var names []string
	for i := range all {
		c := &all[i]
		if c.Name == name {
			return c, names, nil
		}
		if c.Type == advTypeRepeater || c.Type == 3 || c.Type == 4 {
			names = append(names, c.Name)
		}
		if fallback == nil && strings.EqualFold(c.Name, name) {
			fallback = c
		}
	}
	if fallback != nil {
		return fallback, names, nil
	}
	return nil, names, fmt.Errorf("контакт %q не найден среди %d записей", name, len(all))
}

// ---------------------------------------------------------------------------
// Запрос телеметрии
// ---------------------------------------------------------------------------

// requestTelemetry отправляет CMD_SEND_TELEMETRY_REQ и возвращает кадр
// RESP_CODE_SENT: [0]=6 [1]=route(0 direct/1 flood) [2:6]=tag [6:10]=est_timeout_ms
func requestTelemetry(l *link, pubKey []byte, deadline time.Time) ([]byte, error) {
	var buf []byte
	if len(pubKey) == 0 {
		// ровно 4 байта -> прошивка отдаёт телеметрию САМОГО компаньона
		buf = []byte{cmdSendTelemetry, 0, 0, 0}
	} else {
		if len(pubKey) != 32 {
			return nil, fmt.Errorf("ожидался 32-байтный pubkey, получено %d", len(pubKey))
		}
		// 0x27 + 3 зарезервированных байта (pubkey читается с cmd_frame[4]) + pubkey
		buf = make([]byte, 0, 36)
		buf = append(buf, cmdSendTelemetry, 0, 0, 0)
		buf = append(buf, pubKey...)
	}
	if err := l.writeFrame(buf); err != nil {
		return nil, err
	}

	f, err := l.waitFor(map[byte]bool{respSent: true, respErr: true, pushTelemetryResp: true}, deadline)
	if err != nil {
		return nil, err
	}
	if f[0] == respErr {
		code := byte(0)
		if len(f) > 1 {
			code = f[1]
		}
		return nil, fmt.Errorf("запрос отклонён: %s", errCodeName(code))
	}
	if f[0] == pushTelemetryResp {
		return f, nil // self-telemetry приходит сразу, без SENT
	}
	if len(f) < 10 {
		return nil, fmt.Errorf("короткий кадр SENT: %d байт", len(f))
	}
	return f, nil
}

// ---------------------------------------------------------------------------
// Разбор Cayenne LPP (LPPDataHelpers.h + CayenneLPP)
// ---------------------------------------------------------------------------

type lppField struct {
	Channel int     `json:"channel"`
	Type    int     `json:"type"`
	Name    string  `json:"name"`
	Unit    string  `json:"unit"`
	Value   float64 `json:"value"`
}

type lppResult struct {
	Fields []lppField `json:"fields"`
	Raw    string     `json:"raw_hex"`
	Tail   string     `json:"unparsed_hex,omitempty"`
}

// beFloat: значения LPP лежат big-endian.
func beFloat(b []byte, mult float64, signed bool) float64 {
	var v uint32
	for _, x := range b {
		v = (v << 8) + uint32(x)
	}
	sign := 1.0
	if signed {
		bit := uint32(1) << (uint(len(b))*8 - 1)
		if v&bit == bit {
			v = (bit << 1) - v
			sign = -1
		}
	}
	return sign * float64(v) / mult
}

// humiditySize — размер поля относительной влажности в байтах.
// В исходниках MeshCore разнобой: getDataSize() даёт 2, readRelativeHumidity()
// читает 1 (шаг 0.5 %). Кодировщик CayenneLPP пишет 1 байт, поэтому дефолт = 1.
var humiditySize = 1

// dataSize возвращает длину поля значения. Для неизвестного типа ok=false:
// парсинг останавливается, а остаток выводится в unparsed_hex — честный обрыв
// лучше, чем молча поехавшие значения (в отличие от getDataSize(), который для
// неизвестного типа возвращает 1 и ломает весь хвост).
func dataSize(t byte) (int, bool) {
	switch t {
		case 0, 1, 102, 120, 142: // digital in/out, presence, percentage, switch
			return 1, true
		case 104: // relative humidity — см. humiditySize
			return humiditySize, true
		case 2, 3, 101, 103, 115, 116, 117, 121, 125, 128, 132:
			return 2, true
		case 135: // colour RGB
			return 3, true
		case 100, 118, 130, 131, 133:
			return 4, true
		case 113, 134: // accelerometer / gyrometer: 3 оси по 2 байта
			return 6, true
		case 136: // GPS: lat(3) lon(3) alt(3)
			return 9, true
	}
	return 0, false
}

func typeName(t byte) (string, string) {
	switch t {
		case 0:
			return "digital_input", ""
		case 1:
			return "digital_output", ""
		case 2:
			return "analog_input", ""
		case 3:
			return "analog_output", ""
		case 100:
			return "generic_sensor", ""
		case 101:
			return "luminosity", "lx"
		case 102:
			return "presence", ""
		case 103:
			return "temperature", "°C"
		case 104:
			return "relative_humidity", "%"
		case 113:
			return "accelerometer", "G"
		case 115:
			return "pressure", "hPa"
		case 116:
			return "voltage", "V"
		case 117:
			return "current", "A"
		case 118:
			return "frequency", "Hz"
		case 120:
			return "percentage", "%"
		case 121:
			return "altitude", "m"
		case 125:
			return "concentration", "ppm"
		case 128:
			return "power", "W"
		case 130:
			return "distance", "m"
		case 131:
			return "energy", "kWh"
		case 132:
			return "direction", "°"
		case 133:
			return "unixtime", "s"
		case 134:
			return "gyrometer", "°/s"
		case 135:
			return "colour", "RGB"
		case 136:
			return "gps", "°"
		case 142:
			return "switch", ""
	}
	return fmt.Sprintf("unknown_%d", t), ""
}

func decodeScalar(b []byte, t byte) float64 {
	switch t {
		case 103: // temperature: 0.1 °C, signed
			return beFloat(b, 10, true)
		case 104: // relative humidity: 0.5 %, unsigned
			return beFloat(b, 2, false)
		case 115: // pressure: 0.1 hPa, unsigned
			return beFloat(b, 10, false)
		case 116: // voltage: 0.01 V, unsigned
			return beFloat(b, 100, false)
		case 117: // current: 0.001 A, signed
			return beFloat(b, 1000, true)
		case 2, 3: // analog in/out: 0.01, signed
			return beFloat(b, 100, true)
		case 121: // altitude: 1 m, signed
			return beFloat(b, 1, true)
		case 130, 131: // distance / energy: 0.001, unsigned
			return beFloat(b, 1000, false)
		default: // luminosity, percentage, power, frequency, direction,
			// concentration, generic_sensor, unixtime, digital, presence, switch
			return beFloat(b, 1, false)
	}
}

func decodeLPP(buf []byte) lppResult {
	res := lppResult{Raw: hex.EncodeToString(buf)}
	pos := 0
	add := func(ch int, t byte, name, unit string, v float64) {
		res.Fields = append(res.Fields, lppField{ch, int(t), name, unit, v})
	}
	truncated := func() lppResult {
		res.Tail = hex.EncodeToString(buf[pos:])
		return res
	}

	for pos+2 <= len(buf) {
		ch := buf[pos]
		typ := buf[pos+1]
		pos += 2
		if ch == 0 { // канал 0 — маркер конца данных
			break
		}
		name, unit := typeName(typ)

		switch typ {
			case 136: // GPS
				n, _ := dataSize(typ)
				if pos+n > len(buf) {
					return truncated()
				}
				add(int(ch), typ, name+".lat", "°", beFloat(buf[pos:pos+3], 10000, true))
				add(int(ch), typ, name+".lon", "°", beFloat(buf[pos+3:pos+6], 10000, true))
				add(int(ch), typ, name+".alt", "m", beFloat(buf[pos+6:pos+9], 100, true))
				pos += n
			case 113, 134: // accelerometer / gyrometer
				n, _ := dataSize(typ)
				if pos+n > len(buf) {
					return truncated()
				}
				mult := 1000.0
				if typ == 134 {
					mult = 100
				}
				for i, ax := range []string{"x", "y", "z"} {
					add(int(ch), typ, name+"."+ax, unit,
					    beFloat(buf[pos+i*2:pos+i*2+2], mult, true))
				}
				pos += n
			case 135: // colour RGB
				if pos+3 > len(buf) {
					return truncated()
				}
				add(int(ch), typ, name+".r", "", float64(buf[pos]))
				add(int(ch), typ, name+".g", "", float64(buf[pos+1]))
				add(int(ch), typ, name+".b", "", float64(buf[pos+2]))
				pos += 3
			default:
				n, ok := dataSize(typ)
				if !ok || pos+n > len(buf) {
					pos -= 2 // откатиться на заголовок, чтобы он попал в unparsed
					return truncated()
				}
				add(int(ch), typ, name, unit, decodeScalar(buf[pos:pos+n], typ))
				pos += n
		}
	}
	return res
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
	}
}

func main() {
	host := flag.String("host", "mesh.lan", "хост компаньона (WiFi/TCP)")
	port := flag.Int("port", 5000, "TCP-порт компаньона")
	self := flag.Bool("self", false, "запросить телеметрию самого компаньона (без радиоэфира)")
	timeout := flag.Float64("timeout", 0, "таймаут ожидания телеметрии, сек (0 = из suggested_timeout)")
	maxAttempts := flag.Int("attempts", 0, "максимум попыток (0 = бесконечно)")
	retry := flag.Float64("retry", 3, "начальная пауза между попытками, сек")
	maxRetry := flag.Float64("max-retry", 60, "потолок паузы при экспоненциальном росте, сек")
	hum := flag.Int("humidity-size", 1, "размер поля влажности в LPP: 1 (шаг 0.5%) или 2 (шаг 0.1%)")
	asJSON := flag.Bool("json", false, "вывод в JSON")
	verbose := flag.Bool("v", false, "дамп кадров в stderr")
	flag.Parse()

	target := ""
	if !*self {
		if flag.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "Использование: meshcore-telemetry [флаги] <имя-репитера>")
			fmt.Fprintln(os.Stderr, "           или: meshcore-telemetry -self")
			flag.PrintDefaults()
			os.Exit(2)
		}
		target = flag.Arg(0)
	}
	if *hum == 1 || *hum == 2 {
		humiditySize = *hum
	} else {
		fmt.Fprintln(os.Stderr, "ОШИБКА: -humidity-size может быть 1 или 2")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	logf := func(format string, a ...any) {
		if !*asJSON {
			fmt.Fprintf(os.Stderr, format+"\n", a...)
		}
	}

	l, err := dial(*host, *port)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ОШИБКА:", err)
		os.Exit(1)
	}
	defer l.conn.Close()
	l.verbose = *verbose

	if err := l.handshake(time.Now().Add(30 * time.Second)); err != nil {
		fmt.Fprintln(os.Stderr, "ОШИБКА инициализации:", err)
		os.Exit(1)
	}

	// pubKey пустой в режиме -self -> прошивка вернёт телеметрию компаньона
	var pubKey []byte
	name := "self"
	if !*self {
		c, candidates, err := findContact(l, target, time.Now().Add(120*time.Second))
		if err != nil {
			fmt.Fprintln(os.Stderr, "ОШИБКА:", err)
			if len(candidates) > 0 {
				fmt.Fprintln(os.Stderr, "Доступные узлы-кандидаты:")
				for _, n := range candidates {
					fmt.Fprintln(os.Stderr, "  ", n)
				}
			}
			os.Exit(1)
		}
		if c.Type != advTypeRepeater {
			logf("ВНИМАНИЕ: %q имеет type=%d (2 = repeater, 3 = room, 4 = sensor)", c.Name, c.Type)
		}
		pubKey = c.PubKey
		name = c.Name
		logf("Найден узел: %s  pubkey=%s", name, hex.EncodeToString(pubKey))
	}

	retryD := time.Duration(*retry * float64(time.Second))
	maxRetryD := time.Duration(*maxRetry * float64(time.Second))

	for attempt := 1; *maxAttempts == 0 || attempt <= *maxAttempts; attempt++ {
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "прервано пользователем")
			os.Exit(130)
		}
		logf("Попытка %d: запрашиваю телеметрию…", attempt)

		sent, err := requestTelemetry(l, pubKey, time.Now().Add(15*time.Second))
		if err != nil {
			logf("  запрос не отправлен: %v", err)
		} else if sent[0] == pushTelemetryResp && len(sent) >= 8 {
			// ответ уже здесь (режим -self)
			printResult(name, hex.EncodeToString(sent[2:8]), attempt,
				    decodeLPP(sent[8:]), *asJSON)
			return
		} else {
			tag := binary.LittleEndian.Uint32(sent[2:6])
			suggested := binary.LittleEndian.Uint32(sent[6:10])
			route := "direct"
			if sent[1] == 1 {
				route = "flood"
			}

			// как в meshcore_py: suggested_timeout/800 сек == ms*1.25
			wait := time.Duration(suggested) * time.Millisecond * 5 / 4
			if *timeout > 0 {
				wait = time.Duration(*timeout * float64(time.Second))
			}
			if wait < 5*time.Second {
				wait = 5 * time.Second
			}
			if wait > 5*time.Minute {
				wait = 5 * time.Minute
			}
			logf("  отправлено (%s, tag=%08x), жду %s", route, tag, wait)

			f, err := l.waitFor(map[byte]bool{pushTelemetryResp: true}, time.Now().Add(wait))
			if err == nil && len(f) >= 8 {
				// [0]=0x8B [1]=reserved [2:8]=pubkey_prefix [8:]=LPP (tag вырезан прошивкой)
				printResult(name, hex.EncodeToString(f[2:8]), attempt,
					    decodeLPP(f[8:]), *asJSON)
				return
			}
			if err != nil && !errors.Is(err, errTimeout) {
				logf("  ошибка чтения: %v", err)
			} else if err != nil {
				logf("  телеметрия не получена за %s", wait)
			} else {
				logf("  пустой кадр телеметрии (%d байт)", len(f))
			}
		}

		// экспоненциальный бэкофф с потолком
		if err := sleepCtx(ctx, retryD); err != nil {
			fmt.Fprintln(os.Stderr, "прервано пользователем")
			os.Exit(130)
		}
		retryD *= 2
		if retryD > maxRetryD {
			retryD = maxRetryD
		}
	}

	fmt.Fprintln(os.Stderr, "ОШИБКА: телеметрия не получена за отведённое число попыток")
	os.Exit(1)
}

func printResult(name, prefix string, attempt int, r lppResult, asJSON bool) {
	if asJSON {
		out := struct {
			Node     string     `json:"node"`
			PubKey   string     `json:"pubkey_prefix"`
			Attempts int        `json:"attempts"`
			At       time.Time  `json:"at"`
			Fields   []lppField `json:"fields"`
			RawHex   string     `json:"raw_hex"`
			Unparsed string     `json:"unparsed_hex,omitempty"`
		}{name, prefix, attempt, time.Now(), r.Fields, r.Raw, r.Tail}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}

	fmt.Printf("Телеметрия %s (ключ %s), попытка %d\n", name, prefix, attempt)
	if len(r.Fields) == 0 {
		fmt.Println("  полей нет, raw:", r.Raw)
	}
	for _, f := range r.Fields {
		if f.Unit != "" {
			fmt.Printf("  ch%-3d %-26s %10.2f %s\n", f.Channel, f.Name, f.Value, f.Unit)
		} else {
			fmt.Printf("  ch%-3d %-26s %10.0f\n", f.Channel, f.Name, f.Value)
		}
	}
	if r.Tail != "" {
		fmt.Println("  неразобрано:", r.Tail)
	}
	fmt.Println("  raw:", r.Raw)
}

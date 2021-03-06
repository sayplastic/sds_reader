package climateserv

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/tarm/serial"
)

// var (
// 	version = "dev"
// 	commit  = "none"
// 	date    = "unknown"
// )

type AirQualityData struct {
	Timestamp   time.Time `json:"timestamp"`
	PM25        float64   `json:"pm25"`
	PM10        float64   `json:"pm10"`
	AQIPM25     int       `json:"aqi_pm25"`
	AQICategory string    `json:"aqi_category"`
}

var samples = make([]AirQualityData, 10)

func open_device(device_path string) *serial.Port {
	serial_config := &serial.Config{Name: device_path, Baud: 9600}
	serial_port, err := serial.OpenPort(serial_config)
	if err != nil {
		log.Fatal(err)
	}
	return serial_port
}

func StartReading(device_path string, interval int) {
	for {
		entry, err := read_device(device_path)
		if err != nil {
			log.Printf("%s (PM2.5: %.2f, PM10: %.2f)\n", err, entry.PM25, entry.PM10)
			continue
		}
		log.Printf("PM2.5: %.2f, PM10: %.2f\n", entry.PM25, entry.PM10)
		samples = append(samples, entry)
		samples = samples[1:]
		time.Sleep(time.Duration(interval) * time.Second)
	}
}

func read_device(device_path string) (entry AirQualityData, err error) {
	device := open_device(device_path)
	buffer := make([]byte, 10)
	_, err = device.Read(buffer)
	device.Close()

	if err != nil {
		return AirQualityData{PM25: 0.0, PM10: 0.0, Timestamp: time.Now().UTC()}, err
	}
	PM25 := parse_and_convert(buffer[2], buffer[3])
	PM10 := parse_and_convert(buffer[4], buffer[5])
	AQIPM25, category := CurrentPM25toAQI(PM25)
	entry = AirQualityData{
		PM25:        PM25,
		PM10:        PM10,
		AQIPM25:     AQIPM25,
		AQICategory: category,
		Timestamp:   time.Now().UTC()}
	if entry.PM25 > 400 || entry.PM10 > 400 {
		err = errors.New("crazy value read from the port")
	}
	return entry, err
}

func parse_and_convert(raw_value_low byte, raw_value_high byte) float64 {
	low, _ := strconv.ParseInt(fmt.Sprintf("%d", raw_value_low), 10, 32)
	high, _ := strconv.ParseInt(fmt.Sprintf("%d", raw_value_high), 10, 32)
	return ((float64(high) * 256) + float64(low)) / 10
}

func Serve(port string) {
	http.HandleFunc("/api/v1/air_quality/samples", handle_samples_view)
	http.HandleFunc("/api/v1/air_quality/current", handle_current_view)
	http.ListenAndServe(":"+port, nil)
}

func reply_with_json(payload []byte, writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "application/javascript")
	fmt.Fprint(writer, string(payload))
}

func handle_samples_view(writer http.ResponseWriter, request *http.Request) {
	serialized_samples, _ := json.Marshal(samples)
	reply_with_json(serialized_samples, writer)
}

func handle_current_view(writer http.ResponseWriter, request *http.Request) {
	serialized_current, _ := json.Marshal(samples[len(samples)-1])
	reply_with_json(serialized_current, writer)
}

var descriptions = map[string]string{}

type AQI struct {
	cLow        float64
	cHigh       float64
	idxLow      uint
	idxHigh     uint
	Description string
}

var definedAQIs = []AQI{
	AQI{0, 12.0, 0, 50, "good"},
	AQI{12.1, 35.4, 51, 100, "moderate"},
	AQI{35.5, 55.4, 101, 150, "unhealthy for sensitive groups"},
	AQI{55.5, 150.4, 151, 200, "unhealthy"},
	AQI{150.5, 250.4, 201, 300, "very unhealthy"},
	AQI{250.5, 350.4, 301, 400, "hazardous"},
	AQI{350.5, 500.4, 401, 500, "hazardous"},
}

// CurrentPM25toAQI converts current PM2.5 concentration to AQI.
// Code based on AirNow calculator (https://airnow.gov/index.cfm?action=airnow.calculator)
func CurrentPM25toAQI(concentration float64) (int, string) {
	concentration = math.Floor(concentration*10) / 10
	for _, idx := range definedAQIs {
		if idx.cHigh >= concentration && concentration >= idx.cLow {
			return pm25toIdx(idx, concentration), idx.Description
		}
	}

	return 999, "out of range"
}

// pm25toIdx converts PM2.5 concentration to AQI number based on current concentration
// and previous high and low values using EPA formula
// (https://en.wikipedia.org/wiki/Air_quality_index#Computing_the_AQI)
func pm25toIdx(a AQI, concentration float64) int {
	r := (float64(a.idxHigh-a.idxLow)/(a.cHigh-a.cLow))*(concentration-a.cLow) + float64(a.idxLow)
	return int(math.Round(r))
}

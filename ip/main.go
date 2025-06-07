package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/oschwald/geoip2-golang"
	"github.com/yinheli/qqwry"
)

type GeoLocation struct {
	IP          string  `json:"ip"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	Region      string  `json:"region,omitempty"`
	City        string  `json:"city"`
	ISP         string  `json:"isp,omitempty"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Source      string  `json:"source"`
	Error       string  `json:"error,omitempty"`
}

var (
	cityDB  *geoip2.Reader
	asnDB   *geoip2.Reader
	qqwryDB *qqwry.QQwry
)

func initDBs() error {
	// 初始化MaxMind City数据库
	cityPath := os.Getenv("CITY_DB_PATH")
	if cityPath == "" {
		cityPath = "./GeoLite2-City.mmdb"
	}

	var err error
	cityDB, err = geoip2.Open(cityPath)
	if err != nil {
		return fmt.Errorf("failed to open City DB: %v", err)
	}

	// 初始化MaxMind ASN数据库
	asnPath := os.Getenv("ASN_DB_PATH")
	if asnPath == "" {
		asnPath = "./GeoLite2-ASN.mmdb"
	}

	asnDB, err = geoip2.Open(asnPath)
	if err != nil {
		log.Printf("Warning: ASN database not loaded (ISP info will be limited): %v", err)
	}

	// 初始化纯真IP库
	qqwryPath := os.Getenv("QQWRY_PATH")
	if qqwryPath == "" {
		qqwryPath = "./qqwry.dat"
	}

	qqwryDB = qqwry.NewQQwry(qqwryPath)
	log.Println("All databases loaded successfully")
	return nil
}

func queryIP(ip string) GeoLocation {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return GeoLocation{Error: "invalid IP format"}
	}

	// 初始化结果
	result := GeoLocation{
		IP:     ip,
		Source: "MaxMind",
	}

	// City数据库查询
	cityRecord, err := cityDB.City(parsedIP)
	if err == nil {
		result.Country = cityRecord.Country.Names["en"]
		result.CountryCode = cityRecord.Country.IsoCode
		result.Latitude = cityRecord.Location.Latitude
		result.Longitude = cityRecord.Location.Longitude

		if len(cityRecord.Subdivisions) > 0 {
			result.Region = cityRecord.Subdivisions[0].Names["en"]
		}
		if len(cityRecord.City.Names) > 0 {
			result.City = cityRecord.City.Names["en"]
		}
	}

	// ASN数据库查询
	if asnDB != nil {
		asnRecord, err := asnDB.ASN(parsedIP)
		if err == nil {
			result.ISP = asnRecord.AutonomousSystemOrganization
		}
	}

	// 中国IP特殊处理
	if result.CountryCode == "CN" && qqwryDB != nil {
		qqwryDB.Find(ip)
		if qqwryDB.City != "" {
			// 纯真库的City字段实际是ISP信息
			if result.ISP == "" {
				result.ISP = qqwryDB.City
			}
			// 如果MaxMind没有城市信息，使用纯真库的Country作为城市
			if result.City == "" {
				result.City = qqwryDB.Country
			}
			result.Source = "QQWry+MaxMind"
		}
	}

	return result
}

func ipHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ip := strings.TrimPrefix(r.URL.Path, "/")
	if ip == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(GeoLocation{Error: "no IP provided"})
		return
	}

	result := queryIP(ip)
	if result.Error != "" {
		w.WriteHeader(http.StatusInternalServerError)
	}

	json.NewEncoder(w).Encode(result)
}

func main() {
	if err := initDBs(); err != nil {
		log.Fatal(err)
	}
	defer cityDB.Close()
	if asnDB != nil {
		defer asnDB.Close()
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/", ipHandler)
	log.Printf("Server started on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

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
	Province    string  `json:"province,omitempty"`
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
		cityPath = "./data/GeoLite2-City.mmdb"
	}

	var err error
	cityDB, err = geoip2.Open(cityPath)
	if err != nil {
		return fmt.Errorf("failed to open City DB: %v", err)
	}

	// 初始化MaxMind ASN数据库
	asnPath := os.Getenv("ASN_DB_PATH")
	if asnPath == "" {
		asnPath = "./data/GeoLite2-ASN.mmdb"
	}

	asnDB, err = geoip2.Open(asnPath)
	if err != nil {
		log.Printf("Warning: ASN database not loaded (ISP info will be limited): %v", err)
	}

	// 初始化纯真IP库
	qqwryPath := os.Getenv("QQWRY_PATH")
	if qqwryPath == "" {
		qqwryPath = "./data/qqwry.dat"
	}

	qqwryDB = qqwry.NewQQwry(qqwryPath)
	log.Println("All databases loaded successfully")
	return nil
}

// ISP中文化映射
func translateISP(isp string) string {
	ispMap := map[string]string{
		"ZEN-ECN":      "电信",
		"CHINANET":     "电信",
		"CHINATELECOM": "电信",
		"UNICOM":       "联通",
		"CHINA169":     "联通",
		"CMNET":        "移动",
		"CHINAMOBILE":  "移动",
		"ALIBABA":      "阿里云",
		"TENCENT":      "腾讯云",
		"BAIDU":        "百度云",
		"HUAWEI":       "华为云",
		"GOOGLE":       "谷歌",
		"AMAZON":       "亚马逊",
		"MICROSOFT":    "微软",
		"CLOUDFLARE":   "Cloudflare",
		"AKAMAI":       "Akamai",
	}

	if chineseName, exists := ispMap[strings.ToUpper(isp)]; exists {
		return chineseName
	}
	return isp
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
		// 优先使用中文名称，如果没有则使用英文名称
		if countryName, exists := cityRecord.Country.Names["zh-CN"]; exists {
			result.Country = countryName
		} else if countryName, exists := cityRecord.Country.Names["zh"]; exists {
			result.Country = countryName
		} else {
			result.Country = cityRecord.Country.Names["en"]
		}

		result.CountryCode = cityRecord.Country.IsoCode
		result.Latitude = cityRecord.Location.Latitude
		result.Longitude = cityRecord.Location.Longitude

		if len(cityRecord.Subdivisions) > 0 {
			// 优先使用中文名称，如果没有则使用英文名称
			if regionName, exists := cityRecord.Subdivisions[0].Names["zh-CN"]; exists {
				result.Region = regionName
			} else if regionName, exists := cityRecord.Subdivisions[0].Names["zh"]; exists {
				result.Region = regionName
			} else {
				result.Region = cityRecord.Subdivisions[0].Names["en"]
			}
		}

		if len(cityRecord.City.Names) > 0 {
			// 优先使用中文名称，如果没有则使用英文名称
			if cityName, exists := cityRecord.City.Names["zh-CN"]; exists {
				result.City = cityName
			} else if cityName, exists := cityRecord.City.Names["zh"]; exists {
				result.City = cityName
			} else {
				result.City = cityRecord.City.Names["en"]
			}
		}
	}

	// ASN数据库查询
	if asnDB != nil {
		asnRecord, err := asnDB.ASN(parsedIP)
		if err == nil {
			result.ISP = translateISP(asnRecord.AutonomousSystemOrganization)
		}
	}

	// 中国IP特殊处理
	if result.CountryCode == "CN" && qqwryDB != nil {
		qqwryDB.Find(ip)
		if qqwryDB.City != "" {
			// 优先使用纯真库的中文ISP信息
			result.ISP = qqwryDB.City

			// 解析纯真库的Country字段，格式通常是"中国–省份–城市"
			if qqwryDB.Country != "" {
				parts := strings.Split(qqwryDB.Country, "–")
				if len(parts) >= 3 {
					// 第一部分是国家，第二部分是省份，第三部分是城市
					if result.Province == "" {
						result.Province = parts[1]
					}
					if result.City == "" {
						result.City = parts[2]
					}
				} else if len(parts) == 2 {
					// 只有国家和省份的情况
					if result.Province == "" {
						result.Province = parts[1]
					}
				} else {
					// 其他情况，直接作为省份信息
					if result.Province == "" {
						result.Province = qqwryDB.Country
					}
				}
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

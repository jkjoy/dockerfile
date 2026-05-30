package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/go-chi/chi"
	"github.com/oschwald/geoip2-golang"
	"github.com/yinheli/qqwry"
)

type geoLocation struct {
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
}

type ipLookupService struct {
	cityDB    *geoip2.Reader
	asnDB     *geoip2.Reader
	qqwryDB   *qqwry.QQwry
	qqwryMu   sync.Mutex
	cityPath  string
	asnPath   string
	qqwryPath string
}

func newIPLookupService() (*ipLookupService, error) {
	cityPath, _ := configuredFilePath("CITY_DB_PATH",
		"./data/GeoLite2-City.mmdb",
		"../ip/data/GeoLite2-City.mmdb",
		"./ip/data/GeoLite2-City.mmdb",
	)
	if cityPath == "" {
		return nil, fmt.Errorf("city database not found; set CITY_DB_PATH")
	}

	cityDB, err := geoip2.Open(cityPath)
	if err != nil {
		return nil, fmt.Errorf("open city database %q: %w", cityPath, err)
	}

	svc := &ipLookupService{
		cityDB:   cityDB,
		cityPath: cityPath,
	}

	asnPath, asnExplicit := configuredFilePath("ASN_DB_PATH",
		"./data/GeoLite2-ASN.mmdb",
		"../ip/data/GeoLite2-ASN.mmdb",
		"./ip/data/GeoLite2-ASN.mmdb",
	)
	if asnPath != "" {
		asnDB, err := geoip2.Open(asnPath)
		if err != nil {
			if asnExplicit {
				_ = svc.close()
				return nil, fmt.Errorf("open ASN database %q: %w", asnPath, err)
			}
			log.Printf("warning: ASN database not loaded: %v", err)
		} else {
			svc.asnDB = asnDB
			svc.asnPath = asnPath
		}
	}

	qqwryPath, qqwryExplicit := configuredFilePath("QQWRY_PATH",
		"./data/qqwry.dat",
		"../ip/data/qqwry.dat",
		"./ip/data/qqwry.dat",
	)
	if qqwryPath != "" {
		if fileExists(qqwryPath) {
			svc.qqwryDB = qqwry.NewQQwry(qqwryPath)
			svc.qqwryPath = qqwryPath
		} else if qqwryExplicit {
			_ = svc.close()
			return nil, fmt.Errorf("qqwry database %q not found", qqwryPath)
		}
	}

	return svc, nil
}

func configuredFilePath(envName string, candidates ...string) (string, bool) {
	if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
		return value, true
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate, false
		}
	}
	return "", false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (s *ipLookupService) close() error {
	if s == nil {
		return nil
	}
	var err error
	if s.cityDB != nil {
		err = s.cityDB.Close()
	}
	if s.asnDB != nil {
		if closeErr := s.asnDB.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

func (s *ipLookupService) enabled() bool {
	return s != nil && s.cityDB != nil
}

func (s *ipLookupService) query(ip string) (geoLocation, error) {
	if !s.enabled() {
		return geoLocation{}, statusError{status: http.StatusServiceUnavailable, message: "ip lookup service not configured"}
	}

	ip = strings.TrimSpace(ip)
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return geoLocation{}, statusError{status: http.StatusBadRequest, message: "invalid ip format"}
	}

	result := geoLocation{
		IP:     ip,
		Source: "MaxMind",
	}

	cityRecord, err := s.cityDB.City(parsedIP)
	if err == nil {
		result.Country = localizedGeoName(cityRecord.Country.Names)
		result.CountryCode = cityRecord.Country.IsoCode
		result.Latitude = cityRecord.Location.Latitude
		result.Longitude = cityRecord.Location.Longitude

		if len(cityRecord.Subdivisions) > 0 {
			result.Region = localizedGeoName(cityRecord.Subdivisions[0].Names)
		}
		result.City = localizedGeoName(cityRecord.City.Names)
	}

	if s.asnDB != nil {
		asnRecord, err := s.asnDB.ASN(parsedIP)
		if err == nil {
			result.ISP = translateISP(asnRecord.AutonomousSystemOrganization)
		}
	}

	if s.qqwryDB != nil && (result.CountryCode == "CN" || result.CountryCode == "") {
		country, city := s.queryQQWry(ip)
		if city != "" {
			result.ISP = city
		}
		if country != "" {
			applyQQWryLocation(&result, country)
			if result.Country == "" && strings.Contains(country, "中国") {
				result.Country = "中国"
				result.CountryCode = "CN"
			}
		}
		if city != "" || country != "" {
			result.Source = "QQWry+MaxMind"
		}
	}

	return result, nil
}

func (s *ipLookupService) queryQQWry(ip string) (country string, city string) {
	s.qqwryMu.Lock()
	defer s.qqwryMu.Unlock()

	s.qqwryDB.Find(ip)
	return s.qqwryDB.Country, s.qqwryDB.City
}

func localizedGeoName(names map[string]string) string {
	for _, key := range []string{"zh-CN", "zh", "en"} {
		if value := strings.TrimSpace(names[key]); value != "" {
			return value
		}
	}
	return ""
}

func applyQQWryLocation(result *geoLocation, country string) {
	parts := splitQQWryCountry(country)
	if len(parts) >= 3 {
		if result.Province == "" {
			result.Province = parts[1]
		}
		if result.City == "" {
			result.City = parts[2]
		}
		return
	}
	if len(parts) == 2 {
		if result.Province == "" {
			result.Province = parts[1]
		}
		return
	}
	if result.Province == "" {
		result.Province = country
	}
}

func splitQQWryCountry(country string) []string {
	for _, sep := range []string{"–", "-"} {
		if strings.Contains(country, sep) {
			return compactStrings(strings.Split(country, sep))
		}
	}
	return compactStrings([]string{country})
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

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

func requestIPValue(r *http.Request) string {
	if value := strings.TrimSpace(chi.URLParam(r, "ip")); value != "" {
		return value
	}
	if value := strings.TrimSpace(r.PathValue("ip")); value != "" {
		return value
	}
	if value := strings.TrimSpace(r.URL.Query().Get("ip")); value != "" {
		return value
	}
	return ""
}

func clientIPFromRequest(r *http.Request) string {
	for _, header := range []string{"CF-Connecting-IP", "X-Real-IP"} {
		if value := strings.TrimSpace(r.Header.Get(header)); value != "" {
			if ip := net.ParseIP(value); ip != nil {
				return ip.String()
			}
		}
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		for _, part := range strings.Split(forwarded, ",") {
			part = strings.TrimSpace(part)
			if ip := net.ParseIP(part); ip != nil {
				return ip.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

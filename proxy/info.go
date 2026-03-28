package proxies

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"log/slog"

	"github.com/metacubex/mihomo/common/convert"
)

type geoResult struct {
	loc string
	ip  string
}

// GetProxyCountry 并行请求所有 IP 查询端点，按优先级返回最优结果
func GetProxyCountry(httpClient *http.Client) (loc string, ip string) {
	// 顺序代表优先级，索引越小质量越高
	checkers := []func(*http.Client) (string, string){
		GetMe, GetIpinfo, GetCFProxy, GetEdgeOneProxy,
	}

	results := make([]geoResult, len(checkers))
	var wg sync.WaitGroup

	for idx, fn := range checkers {
		wg.Add(1)
		go func(i int, f func(*http.Client) (string, string)) {
			defer wg.Done()
			l, p := f(httpClient)
			results[i] = geoResult{l, p}
		}(idx, fn)
	}

	wg.Wait()

	// 按优先级返回第一个成功的结果
	for _, res := range results {
		if res.loc != "" && res.ip != "" {
			return res.loc, res.ip
		}
	}
	return
}

func GetEdgeOneProxy(httpClient *http.Client) (loc string, ip string) {
	type GeoResponse struct {
		Eo struct {
			Geo struct {
				CountryCodeAlpha2 string `json:"countryCodeAlpha2"`
			} `json:"geo"`
			ClientIp string `json:"clientIp"`
		} `json:"eo"`
	}

	url := "https://functions-geolocation.edgeone.app/geo"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		slog.Debug(fmt.Sprintf("创建请求失败: %s", err))
		return
	}
	req.Header.Set("User-Agent", convert.RandUserAgent())
	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Debug(fmt.Sprintf("edgeone获取节点位置失败: %s", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug(fmt.Sprintf("edgeone返回非200状态码: %v", resp.StatusCode))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Debug(fmt.Sprintf("edgeone读取节点位置失败: %s", err))
		return
	}

	var eo GeoResponse
	err = json.Unmarshal(body, &eo)
	if err != nil {
		slog.Debug(fmt.Sprintf("解析edgeone JSON 失败: %v", err))
		return
	}

	return eo.Eo.Geo.CountryCodeAlpha2, eo.Eo.ClientIp
}

func GetCFProxy(httpClient *http.Client) (loc string, ip string) {
	url := "https://www.cloudflare.com/cdn-cgi/trace"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		slog.Debug(fmt.Sprintf("创建请求失败: %s", err))
		return
	}
	req.Header.Set("User-Agent", convert.RandUserAgent())
	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Debug(fmt.Sprintf("cf获取节点位置失败: %s", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug(fmt.Sprintf("cf返回非200状态码: %v", resp.StatusCode))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Debug(fmt.Sprintf("cf读取节点位置失败: %s", err))
		return
	}

	// Parse the response text to find loc=XX
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "loc=") {
			loc = strings.TrimPrefix(line, "loc=")
		}
		if strings.HasPrefix(line, "ip=") {
			ip = strings.TrimPrefix(line, "ip=")
		}
	}
	return
}

func GetIPSB(httpClient *http.Client) (loc string, ip string) {
	type GeoIPData struct {
		IP      string `json:"ip"`
		Country string `json:"country_code"`
	}

	url := "https://api.ip.sb/geoip"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		slog.Debug(fmt.Sprintf("创建请求失败: %s", err))
		return
	}
	req.Header.Set("User-Agent", convert.RandUserAgent())
	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Debug(fmt.Sprintf("ip.sb获取节点位置失败: %s", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug(fmt.Sprintf("ip.sb返回非200状态码: %v", resp.StatusCode))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Debug(fmt.Sprintf("ip.sb读取节点位置失败: %s", err))
		return
	}

	var geo GeoIPData
	err = json.Unmarshal(body, &geo)
	if err != nil {
		slog.Debug(fmt.Sprintf("解析ip.sb JSON 失败: %v", err))
		return
	}

	return geo.Country, geo.IP
}

func GetMe(httpClient *http.Client) (loc string, ip string) {
	type GeoIPData struct {
		IP      string `json:"ip"`
		Country string `json:"country_code"`
	}

	url := "https://ip.122911.xyz/api/ipinfo"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		slog.Debug(fmt.Sprintf("创建请求失败: %s", err))
		return
	}
	req.Header.Set("User-Agent", "subs-check (https://github.com/beck-8/subs-check)")
	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Debug(fmt.Sprintf("me获取节点位置失败: %s", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug(fmt.Sprintf("me返回非200状态码: %v", resp.StatusCode))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Debug(fmt.Sprintf("me读取节点位置失败: %s", err))
		return
	}

	var geo GeoIPData
	err = json.Unmarshal(body, &geo)
	if err != nil {
		slog.Debug(fmt.Sprintf("解析me JSON 失败: %v", err))
		return
	}

	return geo.Country, geo.IP
}

func GetIpinfo(httpClient *http.Client) (loc string, ip string) {
	type GeoIPData struct {
		IP      string `json:"ip"`
		Country string `json:"country_code"`
	}

	url := string([]byte{104, 116, 116, 112, 115, 58, 47, 47, 97, 112, 105, 46, 105, 112,
		105, 110, 102, 111, 46, 105, 111, 47, 108, 105, 116, 101, 47, 109, 101, 63, 116,
		111, 107, 101, 110, 61, 48, 57, 48, 102, 54, 54, 55, 55, 57, 55, 51, 51, 98, 102})
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		slog.Debug(fmt.Sprintf("创建请求失败: %s", err))
		return
	}
	req.Header.Set("User-Agent", "subs-check (https://github.com/beck-8/subs-check)")
	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Debug(fmt.Sprintf("Ipinfo获取节点位置失败: %s", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug(fmt.Sprintf("Ipinfo返回非200状态码: %v", resp.StatusCode))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Debug(fmt.Sprintf("Ipinfo读取节点位置失败: %s", err))
		return
	}

	var geo GeoIPData
	err = json.Unmarshal(body, &geo)
	if err != nil {
		slog.Debug(fmt.Sprintf("解析Ipinfo JSON 失败: %v", err))
		return
	}

	return geo.Country, geo.IP
}

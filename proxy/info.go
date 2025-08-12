package proxies

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/beck-8/subs-check/config"
	"github.com/metacubex/mihomo/common/convert"
	"github.com/oschwald/maxminddb-golang/v2"
	"github.com/sinspired/checkip/pkg/ipinfo"
)

func GetProxyCountry(httpClient *http.Client, db *maxminddb.Reader, GetAnalyzedCtx context.Context) (loc string, ip string, countryCode_tag string, err error) {
	for i := 0; i < config.GlobalConfig.SubUrlsReTry; i++ {
		// 设置一个临时环境变量，以排除部分api因数据库未更新返回的 CN
		os.Setenv("SUBS-CHECK-CALL", "true")
		defer os.Unsetenv("SUBS-CHECK-CALL")

		// 使用 github.com/sinspired/checkip/pkg/ipinfo API 获取 Analyzed 结果
		cli, err := ipinfo.New(
			ipinfo.WithHttpClient(httpClient),
			ipinfo.WithDBReader(db),
			ipinfo.WithIPAPIs(
				// 可尝试在api后加 /cdn-cgi/trace, 如能返回loc则不可使用.
				// 尽管如此,有些套了 CF的节点,包含了多个出口ip,会导致结果不准
				"https://check.torproject.org/api/ip",
				"https://qifu-api.baidubce.com/ip/local/geo/v1/district",
				"https://r.inews.qq.com/api/ip2city",
				"https://g3.letv.com/r?format=1",
				"https://cdid.c-ctrip.com/model-poc2/h",
				"https://whois.pconline.com.cn/ipJson.jsp",
				"https://api.live.bilibili.com/xlive/web-room/v1/index/getIpInfo",
				"https://6.ipw.cn/",                  // IPv4使用了 CFCDN, IPv6 位置准确
				"https://api6.ipify.org?format=json", // IPv4使用了 CFCDN, IPv6 位置准确
			),
			ipinfo.WithGeoAPIs(
				"https://ident.me/json",
				"https://tnedi.me/json",
				"https://api.seeip.org/geoip",
			),
		)
		if err != nil {
			slog.Debug(fmt.Sprintf("创建 ipinfo 客户端失败: %s", err))
			loc, ip, countryCode_tag, _ = "", "", "", ""
		}
		defer cli.Close()

		// GetAnalyzedCtx 可以安全设置,收到停止信号依然会检测乱序后的前三个api
		// 由于从多个API检测结果,接收到停止信号需要等待更长时间

		// - BadCFNode: HK⁻¹
		// - CFNodeWithSameCountry: HK¹⁺
		// - CFNodeWithDifferentCountry: HK¹-US⁰
		// - NodeWithoutCF: HK²
		// - 前两位字母是实际浏览网站识别的位置, -US⁰为使用CF CDN服务的网站识别的位置, 比如GPT, X等
		loc, ip, countryCode_tag, _ = cli.GetAnalyzed(GetAnalyzedCtx)
		if loc != "" && countryCode_tag != "" {
			slog.Debug(fmt.Sprintf("Analyzed 获取节点位置成功: %s %s", loc, countryCode_tag))
			return loc, ip, countryCode_tag, nil
		}

		// 保留原先获取国家代码的逻辑,但重命名时会添加 ˣ ,例如 HKˣ
		loc, ip = GetMe(httpClient)
		if loc != "" && ip != "" {
			slog.Debug(fmt.Sprintf("me 获取节点位置成功: %s %s", loc, ip))
			return loc, ip, "", nil
		}
		loc, ip = GetIPLark(httpClient)
		if loc != "" && ip != "" {
			slog.Debug(fmt.Sprintf("iplark 获取节点位置成功: %s %s", loc, ip))
			return loc, ip, "", nil
		}
		loc, ip = GetCFProxy(httpClient)
		if loc != "" && ip != "" {
			slog.Debug(fmt.Sprintf("cf 获取节点位置成功: %s %s", loc, ip))
			return loc, ip, "", nil
		}
		// 不准,非常不准
		loc, ip = GetEdgeOneProxy(httpClient)
		if loc != "" && ip != "" {
			slog.Debug(fmt.Sprintf("edgeone 获取节点位置成功: %s %s", loc, ip))
			return loc, ip, "", nil
		}
	}
	return "", "", "", nil
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
	resp, err := httpClient.Get(url)
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
	resp, err := httpClient.Get(url)
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

func GetIPLark(httpClient *http.Client) (loc string, ip string) {
	type GeoIPData struct {
		IP      string `json:"ip"`
		Country string `json:"country_code"`
	}

	url := string([]byte{104, 116, 116, 112, 115, 58, 47, 47, 102, 51, 98, 99, 97, 48, 101, 50, 56, 101, 54, 98, 46, 97, 97, 112, 113, 46, 110, 101, 116, 47, 105, 112, 97, 112, 105, 47, 105, 112, 99, 97, 116})
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		slog.Debug(fmt.Sprintf("创建请求失败: %s", err))
		return
	}
	req.Header.Set("User-Agent", "curl/8.7.1")
	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Debug(fmt.Sprintf("iplark获取节点位置失败: %s", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug(fmt.Sprintf("iplark返回非200状态码: %v", resp.StatusCode))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Debug(fmt.Sprintf("iplark读取节点位置失败: %s", err))
		return
	}

	var geo GeoIPData
	err = json.Unmarshal(body, &geo)
	if err != nil {
		slog.Debug(fmt.Sprintf("解析iplark JSON 失败: %v", err))
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
	req.Header.Set("User-Agent", "subs-check")
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

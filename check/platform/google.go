package platform

import (
	"log/slog"
	"net/http"

)

func CheckGoogle(httpClient *http.Client) (bool, error) {
	if success, err := checkGoogleEndpoint(httpClient, "https://gstatic.com/generate_204", 204); err == nil && success {
		return checkGoogleEndpoint(httpClient, "https://www.google.com/generate_204", 204) 
	}
	return false, nil
}

func CheckGstatic(httpClient *http.Client) (bool, error) {
	if success, err := checkGoogleEndpoint(httpClient, "https://gstatic.com/generate_204", 204); err == nil && success {
		return true, nil
	}
	return false, nil
}


func checkGoogleEndpoint(httpClient *http.Client, url string, statusCode int) (bool, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false, err
	}

	// 添加请求头,模拟正常浏览器访问
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Connection", "close")

	// 发送请求
	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Debug(err.Error())
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode == statusCode, nil
}

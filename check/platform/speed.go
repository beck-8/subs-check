package platform

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"log/slog"

	"github.com/beck-8/subs-check/config"
	"github.com/juju/ratelimit"
	"github.com/metacubex/mihomo/common/convert"
)

// networkLimitedReader 基于网络层字节计数器的大小限制 reader
type networkLimitedReader struct {
	reader       io.Reader
	bytesCounter *uint64
	startBytes   uint64
	limit        uint64
}

func (r *networkLimitedReader) Read(p []byte) (n int, err error) {
	if r.limit > 0 {
		currentBytes := atomic.LoadUint64(r.bytesCounter)
		networkRead := currentBytes - r.startBytes

		if networkRead >= r.limit {
			return 0, io.EOF
		}

		// 限制本次读取的大小（粗略控制，因为网络层可能读取更多）
		if remaining := r.limit - networkRead; remaining < uint64(len(p)) {
			p = p[:remaining]
		}
	}
	return r.reader.Read(p)
}

// CheckSpeed downloads speedTestURL through httpClient and returns the measured
// throughput. The URL is passed in explicitly (rather than read from
// config.GlobalConfig) so a run captured at pipeline start stays consistent
// even if the user edits SpeedTestUrl mid-check.
func CheckSpeed(httpClient *http.Client, bucket *ratelimit.Bucket, bytesCounter *uint64, speedTestURL string) (int, int64, error) {
	// 注意：速度限制在网络层（statsConn）实现，大小限制在应用层基于网络字节计数器实现
	// - 速度限制：通过 bucket 在 statsConn 中实现（网络层）
	// - 大小限制：通过 networkLimitedReader 基于网络字节计数器实现（应用层，但限制网络流量）

	// 创建一个新的测速专用客户端，基于原有客户端的传输层
	speedClient := &http.Client{
		// 设置更长的超时时间用于测速
		Timeout: time.Duration(config.GlobalConfig.DownloadTimeout) * time.Second,
		// 保持原有的传输层配置
		Transport: httpClient.Transport,
	}

	req, err := http.NewRequest("GET", speedTestURL, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("User-Agent", convert.RandUserAgent())

	// 记录测速前的网络传输字节数
	var startBytes uint64
	if bytesCounter != nil {
		startBytes = *bytesCounter
	}
	startTime := time.Now()

	resp, err := speedClient.Do(req)
	if err != nil {
		slog.Debug(fmt.Sprintf("测速请求失败: %v", err))
		return 0, 0, err
	}
	defer resp.Body.Close()

	// 计算网络层的大小限制
	var limitSize uint64
	if config.GlobalConfig.DownloadMB > 0 {
		limitSize = uint64(config.GlobalConfig.DownloadMB) * 1024 * 1024
	} else {
		limitSize = 0 // 不限制
	}

	// 使用 networkLimitedReader 包装响应体，基于网络字节计数器限制大小
	limitedReader := &networkLimitedReader{
		reader:       resp.Body,
		bytesCounter: bytesCounter,
		startBytes:   startBytes,
		limit:        limitSize,
	}

	// 读取所有数据
	totalBytes, err := io.Copy(io.Discard, limitedReader)
	// io.EOF 是正常的（达到限制），其他错误才需要关注
	if err != nil && err != io.EOF && totalBytes == 0 {
		slog.Debug(fmt.Sprintf("totalBytes: %d, 读取数据时发生错误: %v", totalBytes, err))
		return 0, 0, err
	}

	// 计算下载时间（毫秒）
	duration := time.Since(startTime).Milliseconds()
	if duration == 0 {
		duration = 1 // 避免除以零
	}

	// 计算实际网络传输的字节数（压缩数据）
	var actualBytes int64
	if bytesCounter != nil {
		actualBytes = int64(*bytesCounter - startBytes)
	} else {
		// 如果没有字节计数器，无法获取准确数据
		actualBytes = 0
	}

	// 计算速度（KB/s），使用实际网络传输的字节数
	speed := int(float64(actualBytes) / 1024 * 1000 / float64(duration))

	return speed, actualBytes, nil
}

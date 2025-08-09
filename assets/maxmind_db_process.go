package assets

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/beck-8/subs-check/save/method"
	"github.com/klauspost/compress/zstd"
	"github.com/oschwald/maxminddb-golang/v2"
)

// OpenMaxMindDB 使用指定路径打开 MaxMind 数据库
func OpenMaxMindDB(dbPath string) (*maxminddb.Reader, error) {
	var mmdbPath string

	if dbPath != "" {
		// 如果指定了路径，直接使用
		mmdbPath = dbPath
	} else {
		// 否则使用默认路径（output目录）
		saver, err := method.NewLocalSaver()
		if err != nil {
			return nil, err
		}
		if !filepath.IsAbs(saver.OutputPath) {
			saver.OutputPath = filepath.Join(saver.BasePath, saver.OutputPath)
		}

		// 检查输出路径是否可写，如果不可写则使用当前工作目录
		if err := os.MkdirAll(saver.OutputPath, 0755); err != nil {
			// 如果无法创建目录，尝试使用当前工作目录
			cwd, err := os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("无法获取当前工作目录: %w", err)
			}
			saver.OutputPath = filepath.Join(cwd, "output")
			if err := os.MkdirAll(saver.OutputPath, 0755); err != nil {
				return nil, fmt.Errorf("无法创建输出目录: %w", err)
			}
		}
		mmdbPath = filepath.Join(saver.OutputPath, "GeoLite2-Country.mmdb")
	}

	// TODO: 应定期更新数据库文件
	if _, err := os.Stat(mmdbPath); err == nil {
		db, err := maxminddb.Open(mmdbPath)
		if err != nil {
			return nil, fmt.Errorf("maxmind数据库打开失败: %w", err)
		}
		return db, nil
	}

	zstdDecoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("zstd解码器创建失败: %w", err)
	}
	defer zstdDecoder.Close()

	mmdbFile, err := os.OpenFile(mmdbPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("maxmind数据库文件创建失败: %w", err)
	}

	zstdDecoder.Reset(bytes.NewReader(EmbeddedMaxMindDB))
	if _, err := io.Copy(mmdbFile, zstdDecoder); err != nil {
		mmdbFile.Close()
		return nil, fmt.Errorf("maxmind数据库文件解压失败: %w", err)
	}

	// 确保文件完全写入磁盘
	if err := mmdbFile.Close(); err != nil {
		return nil, fmt.Errorf("关闭数据库文件失败: %w", err)
	}

	db, err := maxminddb.Open(mmdbPath)
	if err != nil {
		return nil, fmt.Errorf("maxmind数据库打开失败: %w", err)
	}
	return db, nil
}

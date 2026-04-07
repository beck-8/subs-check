package save

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/beck-8/subs-check/check"
	"github.com/beck-8/subs-check/config"
	"github.com/beck-8/subs-check/save/method"
	"github.com/beck-8/subs-check/utils"
	"gopkg.in/yaml.v3"
)

// SaveFunc 定义保存方法的函数签名
type SaveFunc func(data []byte, filename string) error

// SaveConfig 保存检查结果到本地，并可选保存到远程存储
func SaveConfig(results []check.Result) {
	// 提取代理节点并序列化
	allYamlData, err := marshalProxies(results)
	if err != nil {
		slog.Error(fmt.Sprintf("序列化代理数据失败: %v", err))
		return
	}

	// 保存 all.yaml 到本地
	if err := method.SaveToLocal(allYamlData, "all.yaml"); err != nil {
		slog.Error(fmt.Sprintf("保存all.yaml到本地失败: %v", err))
	}

	// 保存历史快照
	if config.GlobalConfig.KeepDays > 0 {
		SaveHistory(allYamlData)
	}

	// 更新 SubStore 并获取衍生文件（mihomo.yaml / base64.txt）
	var mihomoData, base64Data []byte
	if config.GlobalConfig.SubStorePort != "" {
		utils.UpdateSubStore(allYamlData)
		mihomoData = fetchSubStoreData(
			fmt.Sprintf("%s/api/file/%s", utils.BaseURL, utils.MihomoName),
			"mihomo.yaml",
		)
		base64Data = fetchSubStoreData(
			fmt.Sprintf("%s/download/%s?target=V2Ray", utils.BaseURL, utils.SubName),
			"base64.txt",
		)
	}

	// 保存衍生文件到本地
	saveIfNotEmpty(method.SaveToLocal, mihomoData, "mihomo.yaml")
	saveIfNotEmpty(method.SaveToLocal, base64Data, "base64.txt")

	// 保存所有文件到远程（如果配置了远程保存方式）
	if config.GlobalConfig.SaveMethod == "local" {
		return
	}
	remoteSaver, err := newRemoteSaver()
	if err != nil {
		slog.Error(fmt.Sprintf("初始化远程保存方法(%s)失败: %v", config.GlobalConfig.SaveMethod, err))
		return
	}
	saveIfNotEmpty(remoteSaver, allYamlData, "all.yaml")
	saveIfNotEmpty(remoteSaver, mihomoData, "mihomo.yaml")
	saveIfNotEmpty(remoteSaver, base64Data, "base64.txt")
}

// marshalProxies 从检查结果中提取代理并序列化为 YAML
func marshalProxies(results []check.Result) ([]byte, error) {
	proxies := make([]map[string]any, 0, len(results))
	for _, result := range results {
		proxies = append(proxies, result.Proxy)
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("没有可用的代理节点")
	}
	return yaml.Marshal(map[string]any{"proxies": proxies})
}

// fetchSubStoreData 从 SubStore API 获取数据
func fetchSubStoreData(url, name string) []byte {
	resp, err := http.Get(url)
	if err != nil {
		slog.Error(fmt.Sprintf("获取%s请求失败: %v", name, err))
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error(fmt.Sprintf("读取%s失败: %v", name, err))
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		slog.Error(fmt.Sprintf("获取%s失败, 状态码: %d, 错误信息: %s", name, resp.StatusCode, body))
		return nil
	}
	return body
}

// saveIfNotEmpty 当数据非空时执行保存
func saveIfNotEmpty(saver SaveFunc, data []byte, filename string) {
	if len(data) == 0 {
		return
	}
	if err := saver(data, filename); err != nil {
		slog.Error(fmt.Sprintf("保存%s到%s失败: %v", filename, config.GlobalConfig.SaveMethod, err))
	}
}

// newRemoteSaver 根据配置创建远程保存方法
func newRemoteSaver() (SaveFunc, error) {
	switch config.GlobalConfig.SaveMethod {
	case "r2":
		if err := method.ValiR2Config(); err != nil {
			return nil, fmt.Errorf("R2配置不完整: %w", err)
		}
		return method.UploadToR2Storage, nil
	case "gist":
		if err := method.ValiGistConfig(); err != nil {
			return nil, fmt.Errorf("Gist配置不完整: %w", err)
		}
		return method.UploadToGist, nil
	case "webdav":
		if err := method.ValiWebDAVConfig(); err != nil {
			return nil, fmt.Errorf("WebDAV配置不完整: %w", err)
		}
		return method.UploadToWebDAV, nil
	case "s3":
		if err := method.ValiS3Config(); err != nil {
			return nil, fmt.Errorf("S3配置不完整: %w", err)
		}
		return method.UploadToS3, nil
	default:
		return nil, fmt.Errorf("未知的保存方法: %s", config.GlobalConfig.SaveMethod)
	}
}

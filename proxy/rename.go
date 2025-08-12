package proxies

import (
	"strconv"
	"sync"

	"github.com/beck-8/subs-check/config"
)

var (
	counter     = make(map[string]int)
	counterLock = sync.Mutex{}
)

func Rename(name, countryCodeTag string) string {
	flag := CountryCodeToFlag(name)

	key, label := name, name
	if config.GlobalConfig.EnhancedTag {
		if countryCodeTag != "" {
			key, label = countryCodeTag, countryCodeTag
		} else {
			// fallback 添加 "ˣ" 角标, 例如: "HKˣ"
			label = name + "ˣ"
			key = label
		}
	}

	counterLock.Lock()
	counter[key]++
	n := counter[key]
	counterLock.Unlock()

	return flag + label + "_" + strconv.Itoa(n)
}

// ResetRenameCounter 将所有计数器重置为 0
func ResetRenameCounter() {
	counterLock.Lock()
	defer counterLock.Unlock()

	counter = make(map[string]int)
}

func CountryCodeToFlag(code string) string {
	if len(code) != 2 {
		return "❓Other"
	}

	code = string([]rune(code)[0]&^0x20) + string([]rune(code)[1]&^0x20) // 转成大写（ASCII 位运算）

	r1 := rune(code[0]-'A') + 0x1F1E6
	r2 := rune(code[1]-'A') + 0x1F1E6

	return string([]rune{r1, r2})
}

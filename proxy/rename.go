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

func Rename(name string, countryCodeTag string) string {
	counterLock.Lock()
	defer counterLock.Unlock()

	if config.GlobalConfig.EnhancedTag {
		// 即使开启增强标签,依然使用 name 解析国旗, name 实际就是 countryCodeTag 的前两位
		counter[countryCodeTag]++
		return CountryCodeToFlag(name) + countryCodeTag + "_" + strconv.Itoa(counter[countryCodeTag])
	}
	counter[name]++
	return CountryCodeToFlag(name) + name + "_" + strconv.Itoa(counter[name])
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

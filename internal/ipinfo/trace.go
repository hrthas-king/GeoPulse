// Package ipinfo 只解析文本，不运行定时器，也不发网络请求。
package ipinfo

import (
	"fmt"
	"net/netip"
	"strings"
	"time"
)

const MaxBody = 16 * 1024

type Info struct {
	IP      string    `json:"ip"`
	Country string    `json:"country"`
	At      time.Time `json:"at"`
}

// ISO 3166-1 alpha-2；未知、XX、三字码及注入文本均不接受。
const countries = " AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW "

func ValidCountry(s string) bool {
	return len(s) == 2 && s[0] >= 'A' && s[0] <= 'Z' && s[1] >= 'A' && s[1] <= 'Z' && strings.Contains(countries, " "+s+" ")
}

func ParseTrace(body []byte) (Info, error) {
	if len(body) > MaxBody {
		return Info{}, fmt.Errorf("定位响应超过 %d 字节", MaxBody)
	}
	var ip, country string
	for _, line := range strings.Split(string(body), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "ip":
			if ip != "" {
				return Info{}, fmt.Errorf("重复 ip 字段")
			}
			ip = value
		case "loc":
			if country != "" {
				return Info{}, fmt.Errorf("重复 loc 字段")
			}
			country = value
		}
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.Zone() != "" {
		return Info{}, fmt.Errorf("定位响应不包含合法公网 IP")
	}
	if !ValidCountry(country) {
		return Info{}, fmt.Errorf("定位响应不包含合法国家代码")
	}
	return Info{IP: addr.String(), Country: country, At: time.Now()}, nil
}

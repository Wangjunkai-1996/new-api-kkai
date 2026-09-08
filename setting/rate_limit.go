package setting

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var ModelRequestRateLimitEnabled = false
var ModelRequestRateLimitDurationMinutes = 1
var ModelRequestRateLimitCount = 0
var ModelRequestRateLimitSuccessCount = 1000
var ModelRequestRateLimitGroup = map[string][2]int{}
var ModelRequestRateLimitUser = map[string][2]int{}
var ModelRequestRateLimitMutex sync.RWMutex

func ModelRequestRateLimitGroup2JSONString() string {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	jsonBytes, err := common.Marshal(ModelRequestRateLimitGroup)
	if err != nil {
		common.SysLog("error marshalling model request group rate limits: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateModelRequestRateLimitGroupByJSONString(jsonStr string) error {
	limits := make(map[string][2]int)
	if err := common.UnmarshalJsonStr(jsonStr, &limits); err != nil {
		return err
	}
	ModelRequestRateLimitMutex.Lock()
	ModelRequestRateLimitGroup = limits
	ModelRequestRateLimitMutex.Unlock()
	return nil
}

func GetGroupRateLimit(group string) (totalCount, successCount int, found bool) {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	if ModelRequestRateLimitGroup == nil {
		return 0, 0, false
	}

	limits, found := ModelRequestRateLimitGroup[group]
	if !found {
		return 0, 0, false
	}
	return limits[0], limits[1], true
}

func ModelRequestRateLimitUser2JSONString() string {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	jsonBytes, err := common.Marshal(ModelRequestRateLimitUser)
	if err != nil {
		common.SysLog("error marshalling model request user rate limits: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateModelRequestRateLimitUserByJSONString(jsonStr string) error {
	limits, err := parseModelRequestRateLimitUser(jsonStr)
	if err != nil {
		return err
	}
	ModelRequestRateLimitMutex.Lock()
	ModelRequestRateLimitUser = limits
	ModelRequestRateLimitMutex.Unlock()
	return nil
}

// GetUserRateLimit resolves stable user-ID selectors before username selectors.
func GetUserRateLimit(userID int, username string) (totalCount, successCount int, found bool) {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	if userID > 0 {
		if limits, ok := ModelRequestRateLimitUser["id:"+strconv.Itoa(userID)]; ok {
			return limits[0], limits[1], true
		}
	}
	if username != "" {
		if limits, ok := ModelRequestRateLimitUser["username:"+username]; ok {
			return limits[0], limits[1], true
		}
	}
	return 0, 0, false
}

func CheckModelRequestRateLimitGroup(jsonStr string) error {
	checkModelRequestRateLimitGroup := make(map[string][2]int)
	err := common.UnmarshalJsonStr(jsonStr, &checkModelRequestRateLimitGroup)
	if err != nil {
		return err
	}
	for group, limits := range checkModelRequestRateLimitGroup {
		if limits[0] < 0 || limits[1] < 1 {
			return fmt.Errorf("group %s has negative rate limit values: [%d, %d]", group, limits[0], limits[1])
		}
		if limits[0] > math.MaxInt32 || limits[1] > math.MaxInt32 {
			return fmt.Errorf("group %s [%d, %d] has max rate limits value 2147483647", group, limits[0], limits[1])
		}
	}

	return nil
}

func CheckModelRequestRateLimitUser(jsonStr string) error {
	_, err := parseModelRequestRateLimitUser(jsonStr)
	return err
}

func parseModelRequestRateLimitUser(jsonStr string) (map[string][2]int, error) {
	rawLimits := make(map[string][]int)
	if err := common.UnmarshalJsonStr(jsonStr, &rawLimits); err != nil {
		return nil, err
	}
	if rawLimits == nil {
		return nil, fmt.Errorf("user rate limits must be a JSON object")
	}
	limits := make(map[string][2]int, len(rawLimits))
	for selector, values := range rawLimits {
		if len(values) != 2 {
			return nil, fmt.Errorf("user %s rate limit must contain [total, success]", selector)
		}
		limits[selector] = [2]int{values[0], values[1]}
	}
	if err := validateModelRequestRateLimitUser(limits); err != nil {
		return nil, err
	}
	return limits, nil
}

func validateModelRequestRateLimitUser(limits map[string][2]int) error {
	for selector, values := range limits {
		switch {
		case strings.HasPrefix(selector, "id:"):
			rawID := strings.TrimPrefix(selector, "id:")
			id, err := strconv.Atoi(rawID)
			if err != nil || id <= 0 || strconv.Itoa(id) != rawID {
				return fmt.Errorf("invalid user rate limit selector %q: expected id:<positive integer>", selector)
			}
		case strings.HasPrefix(selector, "username:"):
			username := strings.TrimPrefix(selector, "username:")
			if username == "" || strings.TrimSpace(username) != username {
				return fmt.Errorf("invalid user rate limit selector %q: expected username:<name>", selector)
			}
		default:
			return fmt.Errorf("invalid user rate limit selector %q: expected id:<positive integer> or username:<name>", selector)
		}
		if values[0] < 0 || values[1] < 1 {
			return fmt.Errorf("user %s has invalid rate limit values: [%d, %d]", selector, values[0], values[1])
		}
		if values[0] > math.MaxInt32 || values[1] > math.MaxInt32 {
			return fmt.Errorf("user %s [%d, %d] has max rate limits value 2147483647", selector, values[0], values[1])
		}
	}
	return nil
}

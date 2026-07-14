package authz

import (
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/casbin/casbin/v2"
	casbinmodel "github.com/casbin/casbin/v2/model"
	"gorm.io/gorm"
)

var (
	enforcerMu sync.RWMutex
	enforcer   *casbin.SyncedEnforcer
)

const modelText = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act, eft

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = r.sub == p.sub && r.obj == p.obj && r.act == p.act && p.eft == "allow"
`

func Init(db *gorm.DB) error {
	if common.CanRunSchemaMigrations() {
		if err := seedBuiltInRoles(db); err != nil {
			return err
		}
		if err := resetBuiltInRolePolicies(db); err != nil {
			return err
		}
	}

	m, err := casbinmodel.NewModelFromString(modelText)
	if err != nil {
		return err
	}
	e, err := casbin.NewSyncedEnforcer(m, newGormAdapter(db))
	if err != nil {
		return err
	}
	e.EnableAutoSave(true)

	enforcerMu.Lock()
	enforcer = e
	enforcerMu.Unlock()

	if !common.CanRunSchemaMigrations() {
		return nil
	}
	return seedDefaultPolicies()
}

func currentEnforcer() *casbin.SyncedEnforcer {
	enforcerMu.RLock()
	defer enforcerMu.RUnlock()
	return enforcer
}

func ReloadPolicy() error {
	enforcerMu.Lock()
	defer enforcerMu.Unlock()
	if enforcer == nil {
		return fmt.Errorf("authz enforcer is not initialized")
	}
	return enforcer.LoadPolicy()
}

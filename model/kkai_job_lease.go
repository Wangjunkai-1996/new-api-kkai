package model

type KKAIJobLease struct {
	LeaseName  string `gorm:"column:lease_name;primaryKey;size:128"`
	Holder     string `gorm:"column:holder;size:128;not null"`
	LeaseUntil int64  `gorm:"column:lease_until;not null;index"`
	Fence      int64  `gorm:"column:fence;not null"`
	UpdatedAt  int64  `gorm:"column:updated_at;not null"`
}

func (KKAIJobLease) TableName() string {
	return "kkai_job_leases"
}

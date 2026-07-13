package model

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

func TestChannelFieldsAvoidManagedDefaultsOnHotTable(t *testing.T) {
	var cache sync.Map
	parsed, err := schema.Parse(&Channel{}, &cache, schema.NamingStrategy{})
	require.NoError(t, err)

	for _, name := range []string{"base_url", "status_code_mapping"} {
		field := parsed.LookUpField(name)
		require.NotNil(t, field)
		assert.True(t, field.Creatable)
		assert.True(t, field.Updatable)
		assert.True(t, field.Readable)
		assert.False(t, field.HasDefaultValue)
	}
}

func TestChannelBeforeCreateNormalizesNilTextFields(t *testing.T) {
	channel := &Channel{}

	require.NoError(t, channel.BeforeCreate(nil))
	require.NotNil(t, channel.BaseURL)
	assert.Equal(t, "", *channel.BaseURL)
	require.NotNil(t, channel.StatusCodeMapping)
	assert.Equal(t, "", *channel.StatusCodeMapping)
}

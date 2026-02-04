package json

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/util/jsonpath"
)

func TestJSONPathStaticValue(t *testing.T) {
	j := jsonpath.New("test")
	err := j.Parse("{1}")
	require.NoError(t, err)

	data := map[string]interface{}{}
	buf := new(bytes.Buffer)
	err = j.Execute(buf, data)
	require.NoError(t, err)

	require.Equal(t, "1", buf.String())
}

package atom

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNocmpComparability(t *testing.T) {
	tests := []struct {
		desc       string
		give       interface{}
		comparable bool
	}{
		{
			desc: "nocmp struct",
			give: nocmp{},
		},
		{
			desc: "struct with nocmp embedded",
			give: struct{ nocmp }{},
		},
		{
			desc:       "pointer to struct with nocmp embedded",
			give:       &struct{ nocmp }{},
			comparable: true,
		},

		// All exported types must be uncomparable.
		{desc: "Int32", give: Int32{}},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			typ := reflect.TypeOf(tt.give)
			assert.Equalf(t, tt.comparable, typ.Comparable(),
				"type %v comparablity mismatch", typ)
		})
	}
}

// nocmp must not add to the size of a struct in-memory.
func TestNocmpSize(t *testing.T) {
	type x struct{ _ int }

	before := reflect.TypeOf(x{}).Size()

	type y struct {
		_ nocmp
		_ x
	}

	after := reflect.TypeOf(y{}).Size()

	assert.Equal(t, before, after,
		"expected nocmp to have no effect on struct size")
}

// This test will fail to compile if we disallow copying of nocmp.
//
// We need to allow this so that users can do,
//
//   var x atomic.Int32
//   x = atomic.NewInt32(1)
func TestNocmpCopy(t *testing.T) {
	type foo struct{ _ nocmp }

	t.Run("struct copy", func(t *testing.T) {
		a := foo{}
		b := a
		_ = b // unused
	})

	t.Run("pointer copy", func(t *testing.T) {
		a := &foo{}
		b := *a
		_ = b // unused
	})
}

const _badFile = `package atom

import "fmt"

type Int64 struct {
	nocmp

	v int64
}

func shouldNotCompile() {
	var x, y Int64
	fmt.Println(x == y)
}
`

func TestNocmpIntegration(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "nocmp")
	require.NoError(t, err, "unable to set up temporary directory")
	defer os.RemoveAll(tempdir)

	src := filepath.Join(tempdir, "src")
	require.NoError(t, os.Mkdir(src, 0755), "unable to make source directory")

	nocmp, err := ioutil.ReadFile("nocmp.go")
	require.NoError(t, err, "unable to read nocmp.go")

	require.NoError(t,
		ioutil.WriteFile(filepath.Join(src, "nocmp.go"), nocmp, 0644),
		"unable to write nocmp.go")

	require.NoError(t,
		ioutil.WriteFile(filepath.Join(src, "bad.go"), []byte(_badFile), 0644),
		"unable to write bad.go")

	var stderr bytes.Buffer
	cmd := exec.Command("go", "build")
	cmd.Dir = src
	// Forget OS build enviroment and set up a minimal one for "go build"
	// to run.
	cmd.Env = []string{
		"GOCACHE=" + filepath.Join(tempdir, "gocache"),
	}
	cmd.Stderr = &stderr
	require.Error(t, cmd.Run(), "bad.go must not compile")

	assert.Contains(t, stderr.String(),
		"struct containing nocmp cannot be compared")
}

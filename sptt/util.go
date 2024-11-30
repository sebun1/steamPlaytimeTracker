package sptt

import (
	"bytes"
	"fmt"
	"os"

	"github.com/sebun1/steamPlaytimeTracker/log"
)

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func GetEnv(filename string) (map[string]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer file.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(file)
	env := make(map[string]string)
	for i, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || bytes.HasPrefix(line, []byte("#")) {
			continue
		}
		pair := bytes.SplitN(line, []byte("="), 2)
		if len(pair) != 2 {
			log.Warnf("Invalid line in .env:%d \"%s\"", i, string(line))
			continue
		}
		lhs, rhs := bytes.TrimSpace(pair[0]), bytes.TrimSpace(pair[1])
		if len(rhs) > 1 && rhs[0] == '"' && rhs[len(rhs)-1] == '"' {
			rhs = rhs[1 : len(rhs)-1]
		}
		env[string(lhs)] = string(rhs)
	}
	return env, nil
}

func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s\n\t%w", log.Trace(), err)
}

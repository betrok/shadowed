package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

func dump(val interface{}, prefix ...string) string {
	p := ""
	if len(prefix) > 0 {
		p = prefix[0]
	}
	data, _ := json.MarshalIndent(val, p, "  ")
	return string(data)
}

func readFileInsensitive(base, sub string) ([]byte, error) {
	path := base
	fd, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fd.Close()
loop:
	for _, part := range strings.Split(sub, string(filepath.Separator)) {
		part = strings.ToLower(part)
		files, err := fd.Readdirnames(0)
		if err != nil {
			return nil, err
		}

		for _, name := range files {
			if strings.ToLower(name) == part {
				fd.Close()
				path = filepath.Join(path, name)
				fd, err = os.Open(path)
				if err != nil {
					return nil, err
				}
				continue loop
			}
		}
		return nil, os.ErrNotExist
	}
	data, err := ioutil.ReadAll(fd)
	return data, err
}

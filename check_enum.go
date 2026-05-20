package main

import (
	"fmt"
	"io/ioutil"
	"strings"
)

func main() {
	b, _ := ioutil.ReadFile("/home/svc_pmg_testbed_b/meerstetter-go/mecom/catalogues/tec.json")
	s := string(b)
	idx := strings.Index(s, `"id": 104`)
	if idx != -1 {
		end := idx + 500
		if end > len(s) {
			end = len(s)
		}
		fmt.Println(s[idx:end])
	}
}

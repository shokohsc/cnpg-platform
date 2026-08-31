package main

import (
	"fmt"

	"cnpg-manager/internal/kube"
)

func main() {
	k, err := kube.New()
	if err != nil {
		fmt.Println("warning: no cluster config:", err)
	} else {
		fmt.Println("cnpg-manager: kube client ready:", k != nil)
	}
}

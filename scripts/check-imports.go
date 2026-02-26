//go:build ignore

// check-imports validates that architectural import boundaries are respected.
// Run with: go run scripts/check-imports.go
//
// Exit 0 = clean, Exit 1 = violations found.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const modulePrefix = "github.com/crvgilbertson/intentra/"

type rule struct {
	pkg     string
	deny    []string
	denyMsg string
}

var rules = []rule{
	{
		pkg:     "engine/reasoning",
		deny:    []string{"engine/executors", "cmd/", "os/exec"},
		denyMsg: "reasoning must not depend on git execution or CLI",
	},
	{
		pkg:     "engine/executors",
		deny:    []string{"engine/reasoning"},
		denyMsg: "executors must not depend on LLM/reasoning",
	},
	{
		pkg:     "engine/planners",
		deny:    []string{"cmd/", "os/exec"},
		denyMsg: "planners must not import CLI or shell out to git",
	},
	{
		pkg:     "engine/validators",
		deny:    []string{"engine/executors", "engine/reasoning", "cmd/", "os/exec"},
		denyMsg: "validators must not depend on executors, reasoning, or CLI",
	},
}

type goListPkg struct {
	ImportPath string   `json:"ImportPath"`
	Imports    []string `json:"Imports"`
}

func main() {
	out, err := exec.Command("go", "list", "-json", "./...").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "go list failed: %v\n", err)
		os.Exit(1)
	}

	var packages []goListPkg
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var pkg goListPkg
		if err := dec.Decode(&pkg); err != nil {
			fmt.Fprintf(os.Stderr, "JSON decode error: %v\n", err)
			os.Exit(1)
		}
		packages = append(packages, pkg)
	}

	violations := 0
	for _, pkg := range packages {
		shortPkg := strings.TrimPrefix(pkg.ImportPath, modulePrefix)
		for _, r := range rules {
			if !strings.HasPrefix(shortPkg, r.pkg) {
				continue
			}
			for _, imp := range pkg.Imports {
				shortImp := strings.TrimPrefix(imp, modulePrefix)
				for _, deny := range r.deny {
					match := false
					if strings.HasSuffix(deny, "/") {
						match = strings.HasPrefix(shortImp, deny)
					} else if deny == "os/exec" {
						match = imp == "os/exec"
					} else {
						match = shortImp == deny || strings.HasPrefix(shortImp, deny+"/")
					}
					if match {
						fmt.Printf("VIOLATION: %s imports %s\n  Rule: %s\n\n", shortPkg, imp, r.denyMsg)
						violations++
					}
				}
			}
		}
	}

	if violations > 0 {
		fmt.Printf("\n%d import violation(s) found.\n", violations)
		os.Exit(1)
	}

	fmt.Println("All import boundaries clean.")
}

package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"
)

//go:embed template/asserts.tmpl template/tag.tmpl
var content embed.FS

const (
	assertsTemplate = "template/asserts.tmpl"
	tagTemplate     = "template/tag.tmpl"

	assertsFileName = "asserts.go"
	tagFileName     = "tag.go"
)

type data struct {
	LCName string
	UCName string
}

func main() {
	var name string

	flag.StringVar(&name, "n", "example", "the name of your tag you want to create")
	flag.Parse()

	if err := run(name); err != nil {
		log.Fatal(err)
	}
}

func run(name string) error {
	for _, r := range name {
		if !unicode.IsLetter(r) {
			return fmt.Errorf("name has an invalid value: %s must be letters only", name)
		}
	}

	result := data{
		LCName: strings.ToLower(name),
		UCName: strings.ToUpper(name),
	}

	if err := createFileByTemplate(assertsTemplate, filepath.Join(result.LCName, assertsFileName), result); err != nil {
		return err
	}

	return createFileByTemplate(tagTemplate, filepath.Join(result.LCName, tagFileName), result)
}

func createFileByTemplate(tempPath, filename string, data interface{}) error {
	temp, err := template.ParseFS(content, tempPath)
	if err != nil {
		return err
	}

	if err = os.MkdirAll(filepath.Dir(filename), 0770); err != nil {
		return err
	}

	var file *os.File
	if file, err = os.Create(filename); err != nil {
		return err
	}
	defer file.Close()

	return temp.Execute(file, data)
}

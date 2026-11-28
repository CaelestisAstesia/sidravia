package main

import (
	"archive/zip"
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"sidravia/internal/release/qtrepository"
)

func signQtRepository(args []string, input io.Reader, out, stderr io.Writer) int {
	flags := flag.NewFlagSet("signrelease qt-repository", flag.ContinueOnError)
	flags.SetOutput(stderr)
	source := flags.String("input", "", "unsigned Qt repository ZIP")
	destination := flags.String("output", "", "new signed Qt repository ZIP")
	if err := flags.Parse(args); err != nil || *source == "" || *destination == "" || flags.NArg() != 0 {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, err)
		return 1
	}
	archive, err := zip.OpenReader(*source)
	if err != nil {
		return fail(err)
	}
	defer archive.Close()
	index, err := qtrepository.ReviewUnsigned(&archive.Reader)
	if err != nil {
		return fail(err)
	}
	private, err := readSigningKey(input)
	if err != nil {
		return fail(err)
	}
	defer clear(private)
	if err := writeSignedRepository(*destination, &archive.Reader, ed25519.Sign(private, index)); err != nil {
		return fail(err)
	}
	fmt.Fprintln(out, "Qt repository signed")
	return 0
}

func writeSignedRepository(path string, source *zip.Reader, signature []byte) (result error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	original, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	defer func() {
		if result != nil {
			current, err := os.Lstat(path)
			if err == nil && os.SameFile(original, current) {
				_ = os.Remove(path)
			}
		}
	}()
	archive := zip.NewWriter(file)
	for _, member := range source.File {
		if err := archive.Copy(member); err != nil {
			file.Close()
			return err
		}
	}
	writer, err := archive.Create(qtrepository.Signature)
	if err == nil {
		_, err = writer.Write(signature)
	}
	return errors.Join(err, archive.Close(), file.Sync(), file.Close())
}

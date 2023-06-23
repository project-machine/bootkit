package stubby

import (
	"fmt"
	"io"
	"os"

	"github.com/project-machine/bootkit/pkg/obj"
)

func copyFileContents(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return
	}
	err = out.Sync()
	return
}

// Smoosh - create unified kernel image 'uki' from stubby 'stubEfi'
//    with the provided cmdline and using kernel file 'kernel' initramfs file 'initrd'
//  objcopy
//	   "--add-section=.cmdline=${cmdlinef}"
//	   "--change-section-vma=.cmdline=0x30000"
//     "--add-section=.sbat=$sbatf"
//     "--change-section-vma=.sbat=0x50000"
//     "--set-section-alignment=.sbat=512"
//     "--add-section=.linux=$kernel"
//     "--change-section-vma=.linux=0x2000000"
//     "--add-section=.initrd=$initrd"
//     "--change-section-vma=.initrd=0x3000000"
//     "$stubefi" "$output"
func Smoosh(stubEfi string, uki string, cmdline, sbat, kernel, initrd string) error {
	if err := copyFileContents(stubEfi, uki); err != nil {
		return fmt.Errorf("Failed to copy %s -> %s", stubEfi, uki)
	}

	tmpd, err := os.MkdirTemp("", "smoosh-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpd)

	cmdlineFile, err := os.CreateTemp(tmpd, "")
	if err != nil {
		return err
	}

	if _, err := cmdlineFile.Write([]byte(cmdline)); err != nil {
		return err
	}
	cmdlineFile.Close()

	sbatFile, err := os.CreateTemp(tmpd, "")
	if err != nil {
		return err
	}
	if _, err := sbatFile.Write([]byte(sbat)); err != nil {
		return err
	}
	sbatFile.Close()

	sections := []obj.SectionInput{
		{Name: ".cmdline", VMA: 0x30000, Path: cmdlineFile.Name()},
		{Name: ".sbat", VMA: 0x50000, Alignment: 512, Path: sbatFile.Name()},
		{Name: ".linux", VMA: 0x2000000, Path: kernel},
		{Name: ".initrd", VMA: 0x3000000, Path: initrd},
	}

	return obj.SetSections(uki, sections...)
}

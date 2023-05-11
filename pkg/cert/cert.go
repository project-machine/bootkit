package cert

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	efi "github.com/canonical/go-efilib"
)

func CertFromPemFile(path string) (*x509.Certificate, error) {
	pemData, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return CertFromPem(pemData)
}

func CertFromPem(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("No PEM block found")
	}
	return x509.ParseCertificate(block.Bytes)
}

func GUIDFromFile(path string) (efi.GUID, error) {
	empty := efi.GUID{}
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return empty, err
	}
	return efi.DecodeGUIDString(strings.TrimRight(string(content), "\n"))
}

func LoadSignatureDataDir(dirPath string) (*efi.SignatureData, error) {
	cert, err := CertFromPemFile(filepath.Join(dirPath, "cert.pem"))
	if err != nil {
		return nil, err
	}

	owner, err := GUIDFromFile(filepath.Join(dirPath, "guid"))
	if err != nil {
		return nil, err
	}

	return &efi.SignatureData{Owner: owner, Data: cert.Raw}, nil
}

func LoadSignatureDataDirs(dirPaths ...string) ([]*efi.SignatureData, error) {
	sigs := []*efi.SignatureData{}
	for _, d := range dirPaths {
		curData, err := LoadSignatureDataDir(d)
		if err != nil {
			return sigs, err
		}
		sigs = append(sigs, curData)
	}
	return sigs, nil
}

// SignatureDatabase is just a slice of SignatureList
// SignatureList has multiple SignatureData in .Signatures
//    * each of its Signatures must be the same size
//    * efi.CertX509Guid is the Type that is used for shim db
// SignatureData is a single guid + cert
// This SignatureDatabase is the same as you would get with:
//    ( cert-to-efi-sig-list -g X cert.pem ; cert-to-efi-sig-list -g Y cert2.pem ... ) > my.esl
func NewEFISignatureDatabase(sigDatam []*efi.SignatureData) efi.SignatureDatabase {
	sigdb := efi.SignatureDatabase{}
	for _, sigdata := range sigDatam {
		sigdb = append(sigdb,
			&efi.SignatureList{
				Type:       efi.CertX509Guid,
				Signatures: []*efi.SignatureData{sigdata},
			},
		)
	}
	return sigdb
}

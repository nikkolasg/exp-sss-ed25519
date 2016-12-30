package main

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"

	"golang.org/x/crypto/ed25519"
	"golang.org/x/crypto/openpgp/packet"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	if exists(defaultTmpDir) {
		os.RemoveAll(defaultTmpDir)
	}
	os.Mkdir(defaultTmpDir, 0777)
	ret := m.Run()
	//os.RemoveAll("test")
	os.Exit(ret)
}

type lineWriter struct {
	bytes.Buffer
}

func (l *lineWriter) WriteLine(line string) {
	l.WriteString(line + "\n")
}

var batch = `%no-protection
Key-Type: eddsa
Key-Curve: Ed25519
Name-Real: Joe Tester
Name-Comment: with stupid passphrase
Name-Email: joe@foo.bar
Expire-Date: 1m
# Do a commit here, so that we can later print "done" :-)
%commit
%echo done
`

// generated with gpg --homdir test --expert --full-gen-key --batch foo
var fakeKey = "9458045864343316092B06010401DA470F010107404BBEBF9709835BC984D4206A95A6D83AE70D0A89AE9F2AB3F0A8520CDEDDA6100000FF78FA01D6275B2CBAF194E8604D0224EAC48BA35377271A580297D75E96B03D520FD2B4177465737461203C74657374614074657374612E636F6D3E889604131608003E16210444CB29D2EB4D725FFA38F5BF0C7451265AF77437050258643433021B03050900278D00050B09080702061508090A0B020416020301021E01021780000A09100C7451265AF77437BA250100F6093F29BA030E5E38FB0731221608985D7465E4877942A8F234E9450894769601009DBB4BDE9596E25CDBA3E5149E258F64B46396D3459AA8A6B43E8CD159782208"

func TestCreateEd25519Key(t *testing.T) {
	CreateEd25519Key(batch)

	d, err := os.Open(defaultTmpDir)
	require.Nil(t, err)
	defer d.Close()
	names, err := d.Readdirnames(-1)
	require.Nil(t, err)
	var found bool
	for _, name := range names {
		if strings.Contains(name, "private-keys") {
			found = true
		}
	}
	require.True(t, found)
}

func TestReadEd25519Key(t *testing.T) {
	b, err := hex.DecodeString(fakeKey)
	require.Nil(t, err)
	var buff = bytes.NewBuffer(b)
	p, err := packet.Read(buff)
	require.Nil(t, err)

	priv, ok := p.(*packet.PrivateKey)
	require.True(t, ok)
	// trick to avoid typing to whatever type libraries use..
	require.Equal(t, fmt.Sprintf("%d", PubKeyAlgoEDDSA), fmt.Sprintf("%d", priv.PubKeyAlgo))

}

func TestSplitEd25519Key(t *testing.T) {
	var k = 4
	var n = 6
	priv := readFakePrivateKey(fakeKey)
	ed := priv.PrivateKey.(*ed25519.PrivateKey)
	require.Equal(t, 64, len(*ed))
	var secret PrivateKey
	copy(secret[:], (*ed)[:])

	scalar := secret.Scalar()
	public := scalar.Commit()
	poly, err := NewPoly(rand.Reader, scalar, public, uint32(k))
	require.Nil(t, err)

	shares := make([]Share, n)
	for i := 0; i < int(n); i++ {
		shares[i] = poly.Share(uint32(i))
	}

	recons, err := Reconstruct(shares, uint32(k), uint32(n))
	assert.Nil(t, err)
	assert.True(t, scalar.Equal(recons.Int))
}

func TestSignVerifyEd25519Key(t *testing.T) {
	var msg = []byte("Hello World")
	priv := createAndReadPrivateKey(t)

	var fname = path.Join(defaultTmpDir, "file")
	file, err := os.Create(fname)
	require.Nil(t, err)
	_, err = file.Write(msg)
	require.Nil(t, err)
	require.Nil(t, file.Close())

	sig := sign(t, priv, msg)

	var sigName = path.Join(defaultTmpDir, "testSig")
	f, err := os.Create(sigName)
	require.Nil(t, err)
	err = sig.Serialize(f)
	require.Nil(t, err)
	require.Nil(t, f.Close())

	// try to read with our lib
	buff, err := ioutil.ReadFile(sigName)
	require.Nil(t, err)
	var reader = bytes.NewBuffer(buff)
	unPack, err := packet.Read(reader)
	require.Nil(t, err)

	// try to read with gpg
	_, ok := unPack.(*packet.Signature)
	require.True(t, ok)

	verifyCmd := exec.Command("gpg", "--homedir", defaultTmpDir, "--verify", sigName, fname)
	out, err := verifyCmd.Output()
	if err != nil {
		log.Println(out)
		log.Println(err)
		t.Fail()
	}

	if !strings.Contains(strings.ToLower(string(out)), "good signature") {
		t.Fail()
	}

}

func createAndReadPrivateKey(t *testing.T) *packet.PrivateKey {
	CreateEd25519Key(batch)
	p, err := ReadEd25519Key()
	require.Nil(t, err)
	return p
}

func sign(t *testing.T, priv *packet.PrivateKey, msg []byte) *packet.Signature {
	if priv.PubKeyAlgo != PubKeyAlgoEDDSA {
		t.Fatal("NewSignerPrivateKey should have made a ECSDA private key")
	}

	sig := &packet.Signature{
		PubKeyAlgo:  PubKeyAlgoEDDSA,
		Hash:        crypto.SHA256,
		SigType:     packet.SigTypeBinary,
		IssuerKeyId: &priv.KeyId,
	}

	h := crypto.SHA256.New()
	_, err := h.Write(msg)
	require.Nil(t, err)

	err = sig.Sign(h, priv, nil)
	require.Nil(t, err)

	h = crypto.SHA256.New()
	_, err = h.Write(msg)
	require.Nil(t, err)

	err = priv.VerifySignature(h, sig)
	require.Nil(t, err)

	return sig
}

func readFakePrivateKey(fake string) *packet.PrivateKey {
	b, _ := hex.DecodeString(fake)
	var buff = bytes.NewBuffer(b)
	p, _ := packet.Read(buff)

	priv, _ := p.(*packet.PrivateKey)
	return priv
}

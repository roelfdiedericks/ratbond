#!/bin/sh
#possible targets: "go tool dist list"
go mod vendor

export CGO_ENABLED=0

echo "building mipsle binary"
GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -ldflags "-w" -o "ratbond_mipsle"

echo "building mipsbe binary"
GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -ldflags "-w" -o "ratbond_mipsbe"

echo "building arm64binary"
GOOS=linux GOARCH=arm64 GOMIPS=softfloat go build -ldflags "-w" -o "ratbond_arm64"


echo "building x86 binary"
go build -o "ratbond"

#simply my little post deployment script to deploy to targets, post-build
if test -f ../deployratbond.sh;
then
	../deployratbond.sh
fi

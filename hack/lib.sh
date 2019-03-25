#!/bin/bash

if [ -z "$ROOT" ]; then
    echo "error: ROOT should be initialized"
    exit 1
fi

if [ ! -d "$GOPATH" ]; then
    echo '$GOPATH does not exist'
    exit 1
fi

if [[ :$PATH: == *:$GOPATH/bin:* ]] ; then
    :
else
    export PATH="$GOPATH/bin:$PATH"
fi


OS=$(go env GOOS)
ARCH=$(go env GOARCH)
PLATFORM=$(uname -s | tr A-Z a-z)
OUTPUT=${ROOT}/output
OUTPUT_BIN=${OUTPUT}/bin/${OS}/${ARCH}
DEP_VERSION=0.5.0
DEP_BIN=$OUTPUT_BIN/dep
MISSPELL_VERSION=0.3.4
MISSPELL_BIN=$OUTPUT_BIN/misspell

test -d "$OUTPUT_BIN" || mkdir -p "$OUTPUT_BIN"

function hack::install_protoc() {
    local protoc=$OUTPUT/protoc3/bin/protoc
    if [[ -e $protoc && "$($protoc --version)" == "libprotoc 3."* ]]; then
        return 0
    fi
    local TMPDIR=$(mktemp -d -t goXXXX)
    trap "rm -r $TMPDIR && echo $TMPDIR removed" EXIT
    (
        cd $TMPDIR
        curl -OL https://github.com/google/protobuf/releases/download/v3.3.0/protoc-3.3.0-linux-x86_64.zip
        unzip protoc-3.3.0-linux-x86_64.zip -d protoc3
        ls protoc3
        test -d "$OUTPUT" || mkdir "$OUTPUT"
        mv protoc3 $OUTPUT/
    )
}

function hack::verify_dep() {
    if test -x "$DEP_BIN"; then
        local v=$($DEP_BIN version | awk -F: '/^\s+version\s+:/ { print $2 }' | sed -r 's/^\s+v//g')
        [[ "$v" == "$DEP_VERSION" ]]
        return
    fi
    return 1
}

function hack::install_dep() {
    if hack::verify_dep; then
        return 0
    fi
    platform=$(uname -s | tr A-Z a-z)
    echo "Installing dep v$DEP_VERSION..."
    tmpfile=$(mktemp)
    trap "test -f $tmpfile && rm $tmpfile" RETURN
    wget https://github.com/golang/dep/releases/download/v$DEP_VERSION/dep-${platform}-amd64 -O $tmpfile
    mv $tmpfile $DEP_BIN
    chmod +x $DEP_BIN
}

function hack::verify_misspell() {
    if test -x "$MISSPELL_BIN"; then
        [[ "$($MISSPELL_BIN -v)" == "$MISSPELL_VERSION" ]]
        return
    fi
    return 1
}

function hack::install_misspell() {
    if hack::verify_misspell; then
        return 0
    fi
    echo "Install misspell $MISSPELL_VERSION..."
    local TARURL=https://github.com/client9/misspell/releases/download/v${MISSPELL_VERSION}/misspell_${MISSPELL_VERSION}_linux_64bit.tar.gz
    wget -q $TARURL -O - | tar -zxf - -C "$OUTPUT_BIN"
}

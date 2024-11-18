#!/bin/bash
#set -xe
set -e

vers="v20241112105040_pre"
now=$(date +'%Y-%m-%d_%T')
ldflags="-X main.buildVersion=$vers -X main.buildTime=$now"
go_build_flags=""

function usage() {
  echo "Usage: $0 [-a ARCH] [-o OS] [-t]"
  echo "The supported values of ARCH are '386' (for 32-bit x86), 'amd64' (for 64-bit x86_64) or arm64 (for 64-bit ARM)"
  echo "The supported values of OS are 'linux', 'windows', or 'darwin'"
  echo "The -t option trims the paths in the binaries"
  exit 1
}

GOARCHS=""
GOOSS=""

function parse_opts() {
  while getopts "a:o:t" o
  do
    case "$o" in
      a)
        GOARCHS="$GOARCHS $OPTARG"
        ;;
      o)
        GOOSS="$GOOSS $OPTARG"
        ;;
      t)
        go_build_flags="-trimpath"
        ;;
      *)
        usage
        ;;
    esac
  done
}

function clean() {
  rm -f Rt mdtoc wrap awin aad awin awatch aedit anvsshd adiff ado
  rm -f Rt.exe mdtoc.exe wrap.exe awin.exe aad.exe awin.exe awatch.exe aedit.exe adiff.exe ado.exe
}

function move_if_exists() {
  local src=$1
  local dst=$2

  if [ -f "$src" ]
  then
    mv $src $dst
  fi
}

function save_env_pre_macos_crosscompile() {
  OLDPATH=$PATH
  OLDCC=$CC
  OLDCXX=$CXX
  OLD_CGO_CFLAGS=$CGO_CFLAGS
  OLD_CGO_ENABLED=$CGO_ENABLED
}

function restore_env_post_macos_crosscompile() {
  export PATH=$OLDPATH
  export CC=$OLDCC
  export CXX=$OLDCXX
  export CGO_CFLAGS=$OLD_CGO_CFLAGS
  export CGO_ENABLED=$OLD_CGO_ENABLED
}

function set_env_for_macos_compile() {
  save_env_pre_macos_crosscompile

  if [ "$GOOS" != "darwin" ]
  then
    return
  fi

  # The compiler doesn't handle this function attribute _Nullable_result well.
  # It's used in the Mac SDK, and causes compilation errors. We redefine it to
  # do nothing.
  # Similarly, NS_FORMAT_ARGUMENT which is defined to use the function attribute
  # format_arg(A) also causes problems, so we redefine it as well.
  export CGO_CFLAGS="-D_Nullable_result= -DNS_FORMAT_ARGUMENT(A)= -DTARGET_OS_OSX"
  export CGO_ENABLED=1

  if [ "$DARWIN_CROSS_BIN" = "" ]
  then
    DARWIN_CROSS_BIN=$(realpath ../../../osxcross/target/bin)
  fi

  export PATH=$DARWIN_CROSS_BIN:$PATH

  if [ "$GOARCH" = "amd64" ]
  then
    export CC=x86_64-apple-darwin23.5-cc
    export CXX=x86_64-apple-darwin23.5-c++
  elif [ "$GOARCH" = "arm64" ]
  then
    export CC=arm64-apple-darwin23.5-cc
    export CXX=arm64-apple-darwin23.5-c++
  fi
}

function build() {
  aad_name=aad
  if [ "$GOOS" = "windows" ]
  then
    aad_name=aad.exe
  elif [ "$GOOS" = "darwin" ]
  then
    set_env_for_macos_compile
  fi

  go build -ldflags "$ldflags" $go_build_flags ./cmd/Rt
  go build -ldflags "$ldflags" $go_build_flags ./cmd/mdtoc
  go build -ldflags "$ldflags" $go_build_flags ./cmd/wrap
  go build -o $aad_name -ldflags "$ldflags" $go_build_flags ./cmd/autodump
  go build -ldflags "$ldflags" $go_build_flags ./cmd/awin
  go build -ldflags "$ldflags" $go_build_flags ./cmd/awatch
  go build -ldflags "$ldflags" $go_build_flags ./cmd/aedit
  go build -ldflags "$ldflags" $go_build_flags ./cmd/adiff
  go build -ldflags "$ldflags" $go_build_flags ./cmd/ado
  echo "goos: $GOOS"
  if [ "$GOOS" != "windows" ]
  then
    go build -ldflags "$ldflags" $go_build_flags ./cmd/anvsshd
  else
    echo "not building anvsshd - not supported on windows"
  fi

  if [ "$GOOS" = "darwin" ]
  then
    restore_env_post_macos_crosscompile
  fi
}

function build_all() {
  local msg=$1

  if [ "$msg" = "" ]
  then
    msg="native os and arch"
  fi

  echo "Building anvil-extras for $msg"
  build
}

function build_all_arch() {
  local msg=$1

  if [ "$GOARCHS" = "" ]
  then
    build_all "$msg"
    return
  fi

  for x in $GOARCHS
  do
    if [ "$msg" = "" ]
    then
      msg="arch: $x"
    else
      msg="$msg, arch: $x"
      echo $msg
    fi

    export GOARCH=$x
    build_all "$msg"
  done
}

function build_all_os_and_arch() {
  if [ "$GOOSS" = "" ]
  then
    build_all_arch
    return
  fi

  for x in $GOOSS
  do
    export GOOS=$x
    build_all_arch "os: $x"
  done
}

parse_opts $@

clean

echo "building version $vers"
build_all_os_and_arch

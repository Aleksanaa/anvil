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
  rm -f anvil.exe anvil-con.exe anvil
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

function build_anvil() {
  pushd src/anvil > /dev/null

  rm -f anvil anvil.exe anvil-con.exe
  if [ "$GOOS" = "windows" ]
  then
    gogio -ldflags "$ldflags" -icon ../../img/anvil32b.png -buildmode exe -target windows .
    go build -o anvil-con.exe -ldflags "$ldflags" $go_build_flags
  elif [ "$GOOS" = "darwin" ]
  then
    set_env_for_macos_compile
    go build -ldflags "$ldflags" $go_build_flags
    rcodesign_errmsg="rcodesign not available, so will not codesign binary.\n"
    rcodesign_errmsg="$rcodesign_errmsg if you are compiling natively on darwin and not cross compiling, this is fine."
    rcodesign sign anvil || echo $rcodesign_errmsg
    restore_env_post_macos_crosscompile
  else
    go build -ldflags "$ldflags" $go_build_flags
  fi

  popd > /dev/null

  move_if_exists src/anvil/anvil.exe .
  move_if_exists src/anvil/anvil-con.exe .
  move_if_exists src/anvil/anvil .
}

function build_all() {
  local msg=$1

  if [ "$msg" = "" ]
  then
    msg="native os and arch"
  fi

  echo "Building anvil for $msg"
  build_anvil
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

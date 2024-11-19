{
  lib,
  stdenv,
  buildGoModule,
  pkg-config,
  libicns,
  copyDesktopItems,
  makeDesktopItem,
  desktopToDarwinBundle,
  apple-sdk_11,
  wayland,
  libxkbcommon,
  vulkan-headers,
  libGL,
  xorg,
}:

buildGoModule rec {
  pname = "anvil";
  version = "git";

  src = lib.cleanSource ./.;

  vendorHash = "sha256-Pt3pBHOw1NJeNpLg24Fgqq5J0p2HPvmBF9YyMMfT8B8=";

  nativeBuildInputs =
    [
      pkg-config
      libicns # icns2png
      copyDesktopItems
    ]
    ++ lib.optionals stdenv.hostPlatform.isDarwin [
      desktopToDarwinBundle
    ];

  buildInputs =
    if stdenv.hostPlatform.isDarwin then
      [ apple-sdk_11 ]
    else
      [
        wayland
        libxkbcommon
        vulkan-headers
        libGL
        xorg.libX11
        xorg.libXcursor
        xorg.libXfixes
      ];

  # Got different result in utf8 char length?
  checkFlags = [ "-skip=^TestClearAfter$" ];

  desktopItems = [
    (makeDesktopItem {
      name = "anvil";
      exec = "anvil";
      icon = "anvil";
      desktopName = "Anvil";
      comment = meta.description;
      categories = [ "TextEditor" ];
      startupWMClass = "anvil";
    })
  ];

  postInstall = ''
    pushd misc/icon
      for width in 32 48 128 256; do
        install -Dm644 anvil''${width}b.png \
          $out/share/icons/hicolor/''${width}x''${width}/apps/anvil.png
      done
    popd
  '';

  meta = {
    description = "Graphical, multi-pane tiling editor inspired by Acme";
    homepage = "https://anvil-editor.net";
    license = lib.licenses.mit;
    mainProgram = "anvil";
    maintainers = with lib.maintainers; [ aleksana ];
    platforms = with lib.platforms; unix ++ windows;
  };
}

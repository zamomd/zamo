{
  lib,
  stdenvNoCC,
  buildGo127Module,
  bun2nix,
  nodejs,
  pkg-config,
  wrapGAppsHook4,
  gtk4,
  webkitgtk_6_0,
  glib-networking,
  git,
  versionCheckHook,
  version ? "dev",
}:

let
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../main.go
      ../tray.go
      ../internal
      ../frontend/dist
      ../build/appicon.png
      ../build/linux/desktop
    ];
  };

  frontend = stdenvNoCC.mkDerivation {
    pname = "zamo-frontend";
    inherit version;
    src = ../frontend;
    nativeBuildInputs = [
      bun2nix.hook
      nodejs
    ];
    bunDeps = bun2nix.fetchBunDeps {
      bunNix = ../frontend/bun.nix;
      patchShebangs = false;
    };
    bunInstallFlags = [
      "--linker=hoisted"
      "--backend=copyfile"
      "--frozen-lockfile"
    ];
    dontRunLifecycleScripts = true;
    buildPhase = ''
      runHook preBuild
      chmod -R u+w node_modules
      patchShebangs node_modules
      bun run build
      runHook postBuild
    '';
    installPhase = ''
      runHook preInstall
      cp -R dist $out
      runHook postInstall
    '';
  };
in
buildGo127Module {
  pname = "zamo";
  inherit version src;

  proxyVendor = true;
  overrideModAttrs = _: {
    name = "zamo-go-modules-${
      builtins.substring 0 16 (
        builtins.hashString "sha256" (builtins.readFile ../go.mod + builtins.readFile ../go.sum)
      )
    }";
    # Fetching modules needs no frontend.
    preBuild = "";
  };
  # `just nix-hashes` refreshes it after go.mod or go.sum changes.
  vendorHash = "sha256-O5DXTrDj+QHEgwP50FgD2JamuqIqMi2xyggpAk+IKl0=";

  tags = [ "production" ];
  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}"
  ];

  nativeBuildInputs = [
    pkg-config
    wrapGAppsHook4
  ];
  buildInputs = [
    gtk4
    webkitgtk_6_0
    glib-networking
  ];
  nativeCheckInputs = [ git ];
  nativeInstallCheckInputs = [ versionCheckHook ];
  doInstallCheck = true;

  preBuild = ''
    rm -rf frontend/dist
    cp -R ${frontend} frontend/dist
  '';

  preCheck = ''
    export HOME=$TMPDIR
  '';

  postInstall = ''
    install -Dm644 build/appicon.png $out/share/pixmaps/zamo.png
    install -Dm644 build/linux/desktop $out/share/applications/zamo.desktop
    substituteInPlace $out/share/applications/zamo.desktop \
      --replace-fail "Exec=/usr/local/bin/zamo" "Exec=zamo"
  '';

  passthru = { inherit frontend; };

  meta = {
    description = "Living knowledge base: AI proposes, the human reviews";
    homepage = "https://github.com/zamomd/zamo";
    license = lib.licenses.agpl3Only;
    mainProgram = "zamo";
    platforms = lib.platforms.linux;
  };
}

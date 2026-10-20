{
  description = "API & CLI for System Resources";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    { self, nixpkgs }:
    let
      goModVersion =
        let
          content = builtins.readFile ./go.mod;
          lines = builtins.filter builtins.isString (builtins.split "\n" content);
          goLines = builtins.filter (l: builtins.match "go [0-9]+\\..*" l != null) lines;
          matched =
            if goLines != [ ] then builtins.match "go ([0-9]+)\\.([0-9]+).*" (builtins.head goLines) else null;
        in
        if matched != null then
          {
            major = builtins.elemAt matched 0;
            minor = builtins.elemAt matched 1;
          }
        else
          {
            major = "1";
            minor = "25";
          };
      goForPkgs = pkgs: pkgs.${"go_${goModVersion.major}_${goModVersion.minor}"};

      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];

      forAllSystems =
        f:
        builtins.listToAttrs (
          map (system: {
            name = system;
            value = f system;
          }) supportedSystems
        );

    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
          inherit (pkgs) lib;
        in
        {
          dgop = (pkgs.buildGoModule.override { go = goForPkgs pkgs; }) (finalAttrs: {
            pname = "dgop";
            version = "2.0.0";
            src = ./.;
            vendorHash = "sha256-bVrNwxylrKkmDWdrw+G/yi3DgVdRZtOVn53HfmpXjJk=";

            ldflags = [
              "-s"
              "-w"
              "-X main.Version=${finalAttrs.version}"
              "-X main.buildTime=1970-01-01_00:00:00"
              "-X main.Commit=${finalAttrs.version}"
            ];

            nativeBuildInputs = with pkgs; [
              installShellFiles
              makeWrapper
            ];

            installPhase = ''
              mkdir -p $out/bin
              cp $GOPATH/bin/dgop $out/bin/dgop
              wrapProgram $out/bin/dgop --prefix PATH : "${lib.makeBinPath [ pkgs.pciutils ]}"

              installShellCompletion --cmd dgop \
                  --bash <($out/bin/dgop completion bash) \
                  --fish <($out/bin/dgop completion fish) \
                  --zsh <($out/bin/dgop completion zsh)
            '';

            meta = {
              description = "API & CLI for System Resources";
              homepage = "https://github.com/duxlabs-dev/dux-gop";
              mainProgram = "dgop";
              binaryNativeCode = true;
              license = lib.licenses.mit;
              platforms = lib.platforms.unix;
              maintainers = with lib.maintainers; [ lonerOrz ];
            };
          });

          default = self.packages.${system}.dgop;
        }
      );
    };
}

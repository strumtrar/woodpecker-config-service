{
  description = "Woodpecker CI external configuration service";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    utils.url = "github:numtide/flake-utils";

    # <https://github.com/nix-systems/nix-systems>
    systems.url = "github:nix-systems/default-linux";
  };

  outputs = inputs @ {
    self,
    nixpkgs,
    systems,
    utils,
    ...
  }: let
    inherit (nixpkgs) lib;
    eachSystem = lib.genAttrs (import systems);
    pkgsFor = eachSystem (system:
      import nixpkgs {
        localSystem = system;
        overlays = with self.overlays; [
	  default
        ];
      });
  in {
    overlays.default = final: prev: {
      woodpecker-config-service = with final;
        buildGoModule {

          pname = "woodpecker-config-service";
          version = "v1.0.0";
          src = fetchFromGitHub {
	    owner = "woodpecker-ci";
	    repo = "example-config-service";
	    rev = "master";
	    sha256 = "sha256-6TWmWlLPn6Es0ILdA68ov7QB32Q8bxPzsbZdKf2oeys=";
	  };
	  vendorSha256 = "sha256-JI8E0KgedI7bjddtyWCOgkZ2YZhcwleCJgB6P/0kT4g=";

	  CGO_ENABLED = 1;

          meta = with lib; {
  	    description = "Woodpecker CI external configuration service";
            homepage = "https://github.com/woodpecker-ci/example-config-service";
            license = licenses.gpl3;
            mainProgram = "example-config-service";
            maintainers = with maintainers; [ strumtrar ];
          };
        };
    };

    packages = eachSystem (system: {
      default = self.packages.${system}.woodpecker-config-service;
      inherit
        (pkgsFor.${system})
        woodpecker-config-service;
    });

    defaultApp = utils.lib.mkApp { drv = self.packages.x86_64-linux.default; };

     # Nixos module
    nixosModules.woodpecker-config-service = { pkgs, lib, config, ... }:
      with lib;
      let cfg = config.services.woodpecker-config-service;
      in {
        meta.maintainers = with lib.maintainers; [ strumtrar ];
        options.services.woodpecker-config-service = moduleOptions;
          environment = lib.mkOption {
            default = { };
            type = lib.types.attrsOf lib.types.str;
            example = lib.literalExpression
              ''
                {
                  CONFIG_SERVICE_PUBLIC_KEY_FILE = "/path/to/key.txt";
                  CONFIG_SERVICE_HOST = "localhost:8000";
                  CONFIG_SERVICE_OVERRIDE_FILTER = "test-*"
                }
              '';
          };
	config = {
	  nixpkgs.overlays = [ self.overlays.default ];
	  systemd.services.woodpecker-config-service = {
	    description = "Woodpecker Configuration Service";
	    wantedBy = [ "multi-user.target" ];
	    serviceConfig = {
	      EnvironmentFile = lib.mkIf (cfg.environmentFile != null) cfg.environmentFile;
              User = "woodpecker-config-service";
	      ExecStart = "${cfg.package}/bin/woodpecker-config-service";
	      Restart = "on-failure";
 	    };
          };
        };
      };
   };
}

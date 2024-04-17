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
          version = "v1.1.0";
          src = ./.;
	  vendorHash = "sha256-cRXXMZ2KUp9A4HhiNrtClKP8FMnQ5YqKd/D3EOwPeSg=";
	  vendorSha256 = "sha256-cRXXMZ2KUp9A4HhiNrtClKP8FMnQ5YqKd/D3EOwPeSg=";

	  CGO_ENABLED = 1;

          meta = with lib; {
  	    description = "Woodpecker CI external configuration service";
            homepage = "https://github.com/strumtrar/woodpecker-config-service";
            license = licenses.gpl3;
            mainProgram = "woodpecker-config-service";
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
        options.services.woodpecker-config-service = {
          package = lib.mkPackageOptionMD pkgs "woodpecker-config-service" {};
          enable = lib.mkEnableOption (lib.mdDoc description);
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
        };
	config = {
	  nixpkgs.overlays = [ self.overlays.default ];
	  systemd.services.woodpecker-config-service = {
	    description = "Woodpecker Configuration Service";
	    wantedBy = [ "multi-user.target" ];
	    environment = cfg.environment // {
		HOME = "/run/woodpecker-config-service";
	    };
	    serviceConfig = {
              User = "woodpecker-config-service";
	      ExecStart = "${cfg.package}/bin/example-config-service";
	      Restart = "on-failure";
 	    };
          };
        };
      };
   };
}

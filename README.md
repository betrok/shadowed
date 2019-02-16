# ShadowEd

An utility for modifying assets of Harebrained Schemes' Shadowrun games.

At the moment it is limited to adding new music or replacing the existing one.

**shadowed** also has some capabilities for exploring generic unity assets files, but specialized tools (such as [UnityPack](https://github.com/HearthSim/UnityPack)) can likely provide a better experience.

## Installation

### From sources

Go 1.2+ is required.

`go get -u github.com/betrok/shadowed`

### Prebuild

See [releases page](https://github.com/betrok/shadowed/releases).

## Usage

### Adding or replacing music

#### 1. Backup the vanilla files

During the procedures following files will be modified (relative to data root of game, e.g. `/home/user/.local/share/Steam/SteamApps/common/Shadowrun Hong Kong/SRHK_Data`, see [wiki](https://shadowrun.gamepedia.com/Game_File_Locations)):
* `mainData`
* `resources.assets`
* `resources.assets.resS`
* `StreamingAssets/ContentPacks/shadowrun_core/data/misc/music.mlib.bytes`

Create a backup copy of them.

#### 2. Unpack existing tracks from assets

`shadowed music-unpack sr_data_dir music_dir`

Command above will place `.ogg` files from `resources.assets` in `sr_data_dir` to `music_dir`. Names of the tracks will be mach ones in the editor.

#### 3. Add or replace music

**Removing is not supported by now.**

See [Shadow-Tune docs](https://github.com/Van-Ziegelstein/Shadow-Tune/wiki/resources.assets.resS#track-format) for format details.

Let's assume we just want to add the Dragonfall music and unpack it on top of the HonkKong one from step 2(same command, only data root is different):

`shadowed music-unpack df_data_dir music_dir`

#### 4. Create modified data files

`shadowed music-pack sr_data_dir music_dir output_dir`

This command will create new files in the `output_dir` directory.

#### 5. Copy new files to the data directory

That is, just copy everything from `output_dir` to the Shadowrun data with replacement.

#### 6. Profit!

New music will be listed in the editor after restart.

### Removing read_only flag from a published UGC

`shadowed cpack-make-writable path/to/project.cpack.bytes`

### Other

See `shadowed --help`.

## Acknowledgments

This utility is based on previous works made by the authors of 
* [Shadowrun Wiki](https://shadowrun.gamepedia.com/Official_Shadowrun_Wiki)
* [Hong Kong Music Replacer](https://www.nexusmods.com/shadowrunhongkong/mods/22)
* [Shadow-Tune](https://github.com/Van-Ziegelstein/Shadow-Tune)
* [DisUnity](https://github.com/ata4/disunity)
* [UnityPack](https://github.com/HearthSim/UnityPack)

## Docs

@TODO Document at least something %)

## License

This project is licensed under the MIT License.

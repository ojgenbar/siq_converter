# Example package

`example_pack/` is a complete SIGame package in siq_converter's YAML form. It has one question of every type, every content item kind, and every answer type (select, number, point).

`example_pack.siq` is the same package already packed, ready to open in SIGame. To rebuild it after editing the folder:

```sh
go build -o siqconv ./cmd/siqconv
./siqconv pack -f example/example_pack       # -> example/example_pack.siq
./siqconv verify example/example_pack.siq
```

## Media credits

All media comes from Wikimedia Commons and is in the **public domain**. Files were resized or re-encoded for size. The audio is the first 20 seconds of the recording. The video was re-encoded to H.264 and split in two at Armstrong's step off the ladder (0:24–0:40 and 0:40–end).

| File | Source | Author |
|---|---|---|
| `Images/starry_night.jpg` | [The Starry Night](https://commons.wikimedia.org/wiki/File:Van_Gogh_-_Starry_Night_-_Google_Art_Project.jpg) | Vincent van Gogh |
| `Images/van_gogh_self_portrait.jpg` | [Self-Portrait](https://commons.wikimedia.org/wiki/File:Vincent_van_Gogh_-_Self-Portrait_-_Google_Art_Project_(454045).jpg) | Vincent van Gogh |
| `Images/world_map.jpg` | [Blue Marble 2002](https://commons.wikimedia.org/wiki/File:Blue_Marble_2002.png) | NASA |
| `Images/logo.png` | [Question mark (black)](https://commons.wikimedia.org/wiki/File:Question_mark_(black).svg) | Tacsipacsi |
| `Audio/beethoven_5.mp3` | [Symphony No. 5, I. Allegro con brio](https://commons.wikimedia.org/wiki/File:Ludwig_van_Beethoven_-_symphony_no._5_in_c_minor,_op._67_-_i._allegro_con_brio.ogg) | Ludwig van Beethoven, Musopen |
| `Video/apollo_11_part1.mp4`, `Video/apollo_11_part2.mp4` | [Apollo 11 Landing - first steps on the moon](https://commons.wikimedia.org/wiki/File:Apollo_11_Landing_-_first_steps_on_the_moon.ogv) | NASA |
| `Images/eiffel_tower_1889.jpg` | [Eiffel tower at Exposition Universelle, Paris, 1889](https://commons.wikimedia.org/wiki/File:Eiffel_tower_at_Exposition_Universelle,_Paris,_1889.jpg) | Neurdein |
| portrait embedded in `Html/eiffel.html` | [Gustave Eiffel 1888 Nadar (cropped)](https://commons.wikimedia.org/wiki/File:Gustave_Eiffel_1888_Nadar_(cropped).jpg) | Nadar |

`Html/eiffel.html` was written for this example. It is self-contained: the portrait is embedded as a `data:` URI, because SIGame renames package files when it unpacks a package, so a page can't link to other package files.

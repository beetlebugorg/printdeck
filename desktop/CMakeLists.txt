# Printdeck for the desktop: the window of printdeck app. It needs Qt 6.5 or later.
#
#   cmake -B build && cmake --build build
#
# Put the printdeck program next to the window program, or set PRINTDECK to its path.
cmake_minimum_required(VERSION 3.21)
project(printdeck-desktop LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)

find_package(Qt6 6.5 REQUIRED COMPONENTS Gui Network Qml Quick Quick3D)

qt_add_executable(printdeck-desktop WIN32 MACOSX_BUNDLE main.cpp deck.cpp deck.h sim3d.cpp sim3d.h)
set_target_properties(printdeck-desktop PROPERTIES OUTPUT_NAME Printdeck)

# The deck UI is drawn by the QML files of the Cura plugin, so the two stay the same.
set(plugin_qml ${CMAKE_CURRENT_SOURCE_DIR}/../slicer/cura-plugin/Printdeck/qml)
set_source_files_properties(${plugin_qml}/DeckCanvas.qml PROPERTIES QT_RESOURCE_ALIAS DeckCanvas.qml)
set_source_files_properties(${plugin_qml}/Connecting.qml PROPERTIES QT_RESOURCE_ALIAS Connecting.qml)
qt_add_resources(printdeck-desktop qml PREFIX / FILES
    Main.qml
    Camera.qml
    Sim3D.qml
    ${plugin_qml}/DeckCanvas.qml
    ${plugin_qml}/Connecting.qml)
# The meshes of the printer, from the CAD of LulzBot (cad/export.py, meshes/README.md).
file(GLOB meshes RELATIVE ${CMAKE_CURRENT_SOURCE_DIR} meshes/*.bin)
qt_add_resources(printdeck-desktop meshes PREFIX / FILES ${meshes} meshes/deck.json)
# The font of the titles of the film (film/).
set(plex ${CMAKE_CURRENT_SOURCE_DIR}/../deck/internal/gfx/fonts/IBMPlexMono-SemiBold.ttf)
set_source_files_properties(${plex} PROPERTIES QT_RESOURCE_ALIAS film/plex.ttf)
qt_add_resources(printdeck-desktop film PREFIX / FILES ${plex} FilmSky.qml FilmTitles.qml film/cura-header.png film/cards.json)

target_link_libraries(printdeck-desktop PRIVATE Qt6::Gui Qt6::Network Qt6::Qml Qt6::Quick Qt6::Quick3D)

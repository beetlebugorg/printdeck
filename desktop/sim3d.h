// The 3D window of the simulated printer (printdeck app -sim): the printer moves as the simulated
// board moves it, and the part grows on the bed. SimFeed reads the state of the simulator from the
// app (/sim/state and /sim/path), and PathGeometry is the printed part.
#pragma once

#include <QByteArray>
#include <QFile>
#include <QImage>
#include <QQuick3DTextureData>
#include <QNetworkAccessManager>
#include <QObject>
#include <QProcess>
#include <QPointer>
#include <QQuick3DGeometry>
#include <QTimer>
#include <QUrl>
#include <QVector3D>

// PathGeometry is the printed part: a strand for each extruding move, as wide as the line of the
// nozzle and as high as a layer, with a top and two sides, in the coordinates of the bed (mm, with
// Y of the scene up).
class PathGeometry : public QQuick3DGeometry
{
    Q_OBJECT
    Q_PROPERTY(int moves READ moves NOTIFY movesChanged)

public:
    explicit PathGeometry(QQuick3DObject *parent = nullptr);

    // add adds moves: 6 numbers each, x0 y0 z0 x1 y1 z1 in the coordinates of the printer.
    void add(const QList<float> &segs);
    // set replaces the moves, with the layer height layer.
    void set(const QList<float> &segs, float layer);
    void clear();
    int moves() const { return m_moves; }
    float layer() const { return m_layer; }

signals:
    void movesChanged();

private:
    void upload();

    QByteArray m_vertices;
    int m_moves = 0;
    float m_lastZ = 0;
    float m_layer = 0.3f; // the height of a strand: the step of Z between layers
    QVector3D m_min, m_max;
};

// MeshGeometry is a mesh of the printer from the CAD of LulzBot (cad/export.py): a file of
// triangles in the resources, each vertex 10 bytes (the position as 3 int16 in steps of 1/50 mm,
// the normal as 3 int8 in steps of 1/127, and 1 byte of padding).
class MeshGeometry : public QQuick3DGeometry
{
    Q_OBJECT
    Q_PROPERTY(QString source READ source WRITE setSource NOTIFY sourceChanged)

public:
    explicit MeshGeometry(QQuick3DObject *parent = nullptr);
    QString source() const { return m_source; }
    void setSource(const QString &source);

signals:
    void sourceChanged();

private:
    QString m_source;
};

// ImageTexture is a texture from a picture: the display of the printer, or the screen of the deck.
class ImageTexture : public QQuick3DTextureData
{
    Q_OBJECT
    Q_PROPERTY(QImage image READ image WRITE setImage NOTIFY imageChanged)

public:
    explicit ImageTexture(QQuick3DObject *parent = nullptr) : QQuick3DTextureData(parent) {}
    QImage image() const { return m_image; }
    void setImage(const QImage &image);

signals:
    void imageChanged();

private:
    QImage m_image;
};

// FilmRecorder writes the frames of the video of the 3D window (desktop/film/) to ffmpeg, as raw
// RGBA, at a fixed rate: each frame is 1/fps of the video. armed is true when the file path.go
// exists: the take starts then. The file can have "FROM TO FPS": the take is the seconds FROM to
// TO of the film at FPS frames a second (0 60 30 when the file is empty).
class FilmRecorder : public QObject
{
    Q_OBJECT
    Q_PROPERTY(bool armed READ armed NOTIFY armedChanged)
    Q_PROPERTY(double from READ from NOTIFY armedChanged)
    Q_PROPERTY(double to READ to NOTIFY armedChanged)
    Q_PROPERTY(int fps READ fps NOTIFY armedChanged)

public:
    explicit FilmRecorder(const QString &path, QObject *parent = nullptr);
    bool armed() const { return m_armed; }
    double from() const { return m_from; }
    double to() const { return m_to; }
    int fps() const { return m_fps; }
    Q_INVOKABLE bool start(int width, int height, int fps);
    Q_INVOKABLE void frame(const QImage &image);
    // motion writes the place of the nozzle at a frame to path.motion.csv, for the sound of the
    // steppers (film/steppers.py).
    Q_INVOKABLE void motion(double x, double y, double z, bool extruding);
    Q_INVOKABLE void finish();

signals:
    void armedChanged();

private:
    QString m_path;
    bool m_armed = false;
    double m_from = 0, m_to = 60;
    int m_fps = 30;
    QTimer m_check;
    QProcess *m_ffmpeg = nullptr;
    QFile m_motion;
    QSize m_size;
    int m_frames = 0;
};

// SimFeed reads the state of the simulator from the app.
class SimFeed : public QObject
{
    Q_OBJECT
    // The live position of the printer, in mm: x and z move the tool head and the gantry, y the bed.
    Q_PROPERTY(QVector3D position READ position NOTIFY stateChanged)
    Q_PROPERTY(bool extruding READ extruding NOTIFY stateChanged)
    Q_PROPERTY(double tool READ tool NOTIFY stateChanged)
    Q_PROPERTY(double toolTarget READ toolTarget NOTIFY stateChanged)
    Q_PROPERTY(double bed READ bed NOTIFY stateChanged)
    Q_PROPERTY(double bedTarget READ bedTarget NOTIFY stateChanged)
    Q_PROPERTY(double fan READ fan NOTIFY stateChanged)
    Q_PROPERTY(QString state READ state NOTIFY stateChanged)
    Q_PROPERTY(QString file READ file NOTIFY stateChanged)
    Q_PROPERTY(bool connected READ connected NOTIFY stateChanged)
    // color is the color of the roll in the printer, "#rrggbb", or "".
    Q_PROPERTY(QString color READ color NOTIFY stateChanged)
    Q_PROPERTY(PathGeometry *path READ path WRITE setPath NOTIFY pathChanged)
    // recent is the strands of the last moments, and the strand that the nozzle lays now.
    Q_PROPERTY(PathGeometry *recent READ recent WRITE setRecent NOTIFY pathChanged)
    // lcd is the picture of the display of the printer (/sim/lcd.png), 4 times a second.
    Q_PROPERTY(QImage lcd READ lcd NOTIFY lcdChanged)
    // screen is the picture of the deck UI: the window of the deck sets it.
    Q_PROPERTY(QImage screen READ screen WRITE setScreen NOTIFY screenChanged)

public:
    explicit SimFeed(QObject *parent = nullptr);

    // start reads the app at base, with the token of the window.
    void start(const QUrl &base, const QByteArray &token);

    QVector3D position() const { return m_pos; }
    bool extruding() const { return m_extruding; }
    double tool() const { return m_tool; }
    double toolTarget() const { return m_toolT; }
    double bed() const { return m_bed; }
    double bedTarget() const { return m_bedT; }
    double fan() const { return m_fan; }
    QString state() const { return m_state; }
    QString file() const { return m_file; }
    bool connected() const { return m_connected; }
    QString color() const { return m_color; }
    PathGeometry *path() const { return m_path; }
    void setPath(PathGeometry *p);
    PathGeometry *recent() const { return m_recent; }
    void setRecent(PathGeometry *p);
    QImage lcd() const { return m_lcd; }
    QImage screen() const { return m_screen; }
    void setScreen(const QImage &image);
    // resource is the JSON of a file of the resources, for example ":/meshes/deck.json".
    Q_INVOKABLE QVariant resource(const QString &path) const;

signals:
    void stateChanged();
    void pathChanged();
    void lcdChanged();
    void screenChanged();

private:
    void pollState();
    void pollPath();
    void pollLCD();
    void follow(bool busy);
    QNetworkReply *get(const QString &path);

    QNetworkAccessManager m_net;
    QUrl m_base;
    QByteArray m_token;
    QTimer m_stateTimer, m_pathTimer, m_lcdTimer;
    bool m_stateBusy = false, m_pathBusy = false, m_lcdBusy = false;
    QImage m_lcd, m_screen;
    QVector3D m_pos;
    bool m_extruding = false, m_connected = false;
    double m_tool = 0, m_toolT = 0, m_bed = 0, m_bedT = 0, m_fan = 0;
    QString m_state, m_file, m_color;
    QPointer<PathGeometry> m_path, m_recent;
    // The moves that the nozzle has not laid yet (6 numbers each), and the moves that it laid
    // since the part took them last.
    QList<float> m_pending, m_laid;
    qint64 m_committed = 0; // the time when the part took the laid moves, in ms
    QVector3D m_last;       // the place of the nozzle at the last state, for the trail
    bool m_hasLast = false;
    QString m_key; // the print of the moves in the path
    int m_next = 0; // the next move to read
};

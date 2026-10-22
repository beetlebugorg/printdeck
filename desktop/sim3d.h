// The 3D window of the simulated printer (printdeck app -sim): the printer moves as the simulated
// board moves it, and the part grows on the bed. SimFeed reads the state of the simulator from the
// app (/sim/state and /sim/path), and PathGeometry is the printed part.
#pragma once

#include <QByteArray>
#include <QNetworkAccessManager>
#include <QObject>
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
    Q_PROPERTY(PathGeometry *path READ path WRITE setPath NOTIFY pathChanged)
    // recent is the strands of the last moments, and the strand that the nozzle lays now.
    Q_PROPERTY(PathGeometry *recent READ recent WRITE setRecent NOTIFY pathChanged)

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
    PathGeometry *path() const { return m_path; }
    void setPath(PathGeometry *p);
    PathGeometry *recent() const { return m_recent; }
    void setRecent(PathGeometry *p);

signals:
    void stateChanged();
    void pathChanged();

private:
    void pollState();
    void pollPath();
    void follow(bool busy);
    QNetworkReply *get(const QString &path);

    QNetworkAccessManager m_net;
    QUrl m_base;
    QByteArray m_token;
    QTimer m_stateTimer, m_pathTimer;
    bool m_stateBusy = false, m_pathBusy = false;
    QVector3D m_pos;
    bool m_extruding = false, m_connected = false;
    double m_tool = 0, m_toolT = 0, m_bed = 0, m_bedT = 0, m_fan = 0;
    QString m_state, m_file;
    QPointer<PathGeometry> m_path, m_recent;
    // The moves that the nozzle has not laid yet (6 numbers each), and the moves that it laid
    // since the part took them last.
    QList<float> m_pending, m_laid;
    qint64 m_committed = 0; // the time when the part took the laid moves, in ms
    QString m_key; // the print of the moves in the path
    int m_next = 0; // the next move to read
};

#include "sim3d.h"

#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QNetworkReply>
#include <QNetworkRequest>
#include <QUrlQuery>
#include <cmath>

namespace {

// The width of a strand: the line of a 0.5 mm nozzle.
constexpr float lineWidth = 0.55f;
// The vertex: a position and a normal, 6 floats.
constexpr int vertexStride = 6 * sizeof(float);

void put(QByteArray &b, QVector3D p, QVector3D n)
{
    const float v[6] = {p.x(), p.y(), p.z(), n.x(), n.y(), n.z()};
    b.append(reinterpret_cast<const char *>(v), sizeof(v));
}

// quad adds two triangles: a b c, a c d, with the normal n.
void quad(QByteArray &b, QVector3D a, QVector3D bb, QVector3D c, QVector3D d, QVector3D n)
{
    put(b, a, n);
    put(b, bb, n);
    put(b, c, n);
    put(b, a, n);
    put(b, c, n);
    put(b, d, n);
}

// scene turns a point of the printer (x right, y back, z up) into the scene (x right, y up, z to
// the viewer).
QVector3D scene(float x, float y, float z) { return QVector3D(x, z, -y); }

} // namespace

PathGeometry::PathGeometry(QQuick3DObject *parent) : QQuick3DGeometry(parent)
{
    setPrimitiveType(PrimitiveType::Triangles);
    setStride(vertexStride);
    addAttribute(Attribute::PositionSemantic, 0, Attribute::F32Type);
    addAttribute(Attribute::NormalSemantic, 3 * sizeof(float), Attribute::F32Type);
}

void PathGeometry::add(const QList<float> &segs)
{
    for (int i = 0; i + 5 < segs.size(); i += 6) {
        float x0 = segs[i], y0 = segs[i + 1], z0 = segs[i + 2], x1 = segs[i + 3], y1 = segs[i + 4], z1 = segs[i + 5];
        // The height of a strand is the step from the layer under it.
        if (z1 > m_lastZ + 0.01f) {
            if (m_lastZ > 0)
                m_layer = std::clamp(z1 - m_lastZ, 0.08f, 0.6f);
            m_lastZ = z1;
        }
        float dx = x1 - x0, dy = y1 - y0, len = std::hypot(dx, dy);
        if (len < 0.01f)
            continue;
        // The side of the strand: half the width, across the move.
        float sx = -dy / len * lineWidth / 2, sy = dx / len * lineWidth / 2;
        float top = z1, bot = z1 - m_layer;
        QVector3D a = scene(x0 + sx, y0 + sy, top), b = scene(x1 + sx, y1 + sy, top);
        QVector3D c = scene(x1 - sx, y1 - sy, top), d = scene(x0 - sx, y0 - sy, top);
        QVector3D ab = scene(x0 + sx, y0 + sy, bot), bb = scene(x1 + sx, y1 + sy, bot);
        QVector3D cb = scene(x1 - sx, y1 - sy, bot), db = scene(x0 - sx, y0 - sy, bot);
        QVector3D side = scene(sx, sy, 0).normalized();
        quad(m_vertices, a, b, c, d, QVector3D(0, 1, 0)); // the top
        quad(m_vertices, ab, bb, b, a, side);              // one side
        quad(m_vertices, c, cb, db, d, -side);             // the other side
        for (QVector3D p : {a, c, bb, db}) {
            if (m_moves == 0 && p == a)
                m_min = m_max = p;
            m_min = QVector3D(std::min(m_min.x(), p.x()), std::min(m_min.y(), p.y()), std::min(m_min.z(), p.z()));
            m_max = QVector3D(std::max(m_max.x(), p.x()), std::max(m_max.y(), p.y()), std::max(m_max.z(), p.z()));
        }
        m_moves++;
    }
    upload();
}

void PathGeometry::clear()
{
    m_vertices.clear();
    m_moves = 0;
    m_lastZ = 0;
    m_layer = 0.3f;
    upload();
}

void PathGeometry::upload()
{
    setVertexData(m_vertices);
    setBounds(m_min, m_max);
    update();
    emit movesChanged();
}

SimFeed::SimFeed(QObject *parent) : QObject(parent)
{
    m_stateTimer.setInterval(33); // 30 times a second: the moves of the printer
    m_pathTimer.setInterval(1000);
    connect(&m_stateTimer, &QTimer::timeout, this, &SimFeed::pollState);
    connect(&m_pathTimer, &QTimer::timeout, this, &SimFeed::pollPath);
}

void SimFeed::start(const QUrl &base, const QByteArray &token)
{
    m_base = base;
    m_token = token;
    m_stateTimer.start();
    m_pathTimer.start();
}

void SimFeed::setPath(PathGeometry *p)
{
    if (m_path == p)
        return;
    m_path = p;
    m_key.clear();
    m_next = 0;
    emit pathChanged();
}

QNetworkReply *SimFeed::get(const QString &path)
{
    QUrl url = m_base.resolved(QUrl(path));
    QNetworkRequest req(url);
    req.setRawHeader("X-Api-Key", m_token);
    req.setTransferTimeout(5000);
    return m_net.get(req);
}

void SimFeed::pollState()
{
    if (m_stateBusy)
        return;
    m_stateBusy = true;
    QNetworkReply *r = get(QStringLiteral("/sim/state"));
    connect(r, &QNetworkReply::finished, this, [this, r] {
        r->deleteLater();
        m_stateBusy = false;
        if (r->error() != QNetworkReply::NoError) {
            if (m_connected) {
                m_connected = false;
                emit stateChanged();
            }
            return;
        }
        QJsonObject o = QJsonDocument::fromJson(r->readAll()).object();
        QJsonArray p = o.value(QStringLiteral("pos")).toArray();
        if (p.size() >= 3)
            m_pos = QVector3D(p[0].toDouble(), p[1].toDouble(), p[2].toDouble());
        m_extruding = o.value(QStringLiteral("ev")).toDouble() > 0.01;
        m_tool = o.value(QStringLiteral("tool")).toDouble();
        m_toolT = o.value(QStringLiteral("toolT")).toDouble();
        m_bed = o.value(QStringLiteral("bed")).toDouble();
        m_bedT = o.value(QStringLiteral("bedT")).toDouble();
        m_fan = o.value(QStringLiteral("fan")).toDouble();
        m_state = o.value(QStringLiteral("state")).toString();
        m_file = o.value(QStringLiteral("file")).toString();
        m_connected = true;
        emit stateChanged();
    });
}

void SimFeed::pollPath()
{
    if (m_pathBusy || !m_path)
        return;
    m_pathBusy = true;
    QUrlQuery q;
    q.addQueryItem(QStringLiteral("key"), m_key);
    q.addQueryItem(QStringLiteral("from"), QString::number(m_next));
    QNetworkReply *r = get(QStringLiteral("/sim/path?") + q.toString(QUrl::FullyEncoded));
    connect(r, &QNetworkReply::finished, this, [this, r] {
        r->deleteLater();
        m_pathBusy = false;
        if (r->error() != QNetworkReply::NoError || !m_path)
            return;
        QJsonObject o = QJsonDocument::fromJson(r->readAll()).object();
        QString key = o.value(QStringLiteral("key")).toString();
        int from = o.value(QStringLiteral("from")).toInt();
        if (key != m_key || from == 0) {
            // A new print, or no print: the part starts again.
            if (key != m_key || m_next != 0)
                m_path->clear();
            m_key = key;
            m_next = 0;
        }
        QJsonArray s = o.value(QStringLiteral("segs")).toArray();
        QList<float> segs;
        segs.reserve(s.size());
        for (const QJsonValue &v : s)
            segs.append(float(v.toDouble()));
        if (!segs.isEmpty())
            m_path->add(segs);
        m_next = from + segs.size() / 6;
        // More moves wait: read them now.
        if (segs.size() / 6 >= 20000)
            QTimer::singleShot(0, this, &SimFeed::pollPath);
    });
}

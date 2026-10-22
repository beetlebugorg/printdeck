// Printdeck for the desktop: a window that shows the deck UI of printdeck app, for a printer on USB.
// The window starts printdeck app, and shows its remote screen (deck.h).
#include <QCoreApplication>
#include <QDir>
#include <QFileInfo>
#include <QGuiApplication>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickWindow>
#include <QTimer>

#include "deck.h"

// printdeckProgram is the path of the printdeck program: $PRINTDECK, or the program next to the
// window program.
static QString printdeckProgram()
{
    QString env = qEnvironmentVariable("PRINTDECK");
    if (!env.isEmpty())
        return env;
#ifdef Q_OS_WIN
    const QString name = QStringLiteral("printdeck.exe");
#else
    const QString name = QStringLiteral("printdeck");
#endif
    return QDir(QCoreApplication::applicationDirPath()).filePath(name);
}

int main(int argc, char *argv[])
{
    QGuiApplication app(argc, argv);
    app.setApplicationName(QStringLiteral("Printdeck"));
    app.setOrganizationName(QStringLiteral("Printdeck"));

    Deck deck;
    QQmlApplicationEngine engine;
    Factory factory(&deck);
    engine.setNetworkAccessManagerFactory(&factory);
    engine.rootContext()->setContextProperty(QStringLiteral("printdeck"), &deck);
    QObject::connect(&engine, &QQmlApplicationEngine::objectCreationFailed, &app,
                     [] { QCoreApplication::exit(1); }, Qt::QueuedConnection);
    engine.load(QUrl(QStringLiteral("qrc:/Main.qml")));

    // -parent: printdeck app ends when the window closes its input.
    QStringList args{QStringLiteral("app"), QStringLiteral("-parent")};
    args += app.arguments().mid(1);
    deck.start(printdeckProgram(), args);
    QObject::connect(&app, &QCoreApplication::aboutToQuit, &deck, &Deck::stop);

    // PRINTDECK_SHOT=FILE.png writes a picture of the window after 8 seconds, and ends the app.
    // It is for the pictures of the manual and for tests.
    QString shot = qEnvironmentVariable("PRINTDECK_SHOT");
    if (!shot.isEmpty()) {
        QTimer::singleShot(8000, &app, [&engine, shot] {
            auto *win = qobject_cast<QQuickWindow *>(engine.rootObjects().value(0));
            if (win)
                win->grabWindow().save(shot);
            QCoreApplication::quit();
        });
    }
    return app.exec();
}

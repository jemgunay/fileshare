$(document).ready(function() {
    //animate burger menu button
    $('#navbar').on('hide.bs.collapse show.bs.collapse', function () {
        $('#nav-animated-icon').toggleClass('open');
    });
});

var hostname = location.protocol + '//' + location.host;
// Create an instance of an AlertNotifier.
var notifier = new AlertNotifier(5, 4000);

// Perform basic AJAX request.
function performRequest(URL, httpMethod, data, resultMethod) {
    console.log("> [", httpMethod, "] ", URL, ": ", data);
    $.ajax({
        url: URL,
        type: httpMethod,
        dataType: 'text',
        data: data,
        error: function(e) {
            console.log(e);
            notifier.queueAlert("Could not connect to the server.", "danger");
        },
        success: function(e) {
            resultMethod(e);
        },
        timeout: 10000
    });
}

// Get HTML for a warning/error HTML message.
function setAlertWindow(type, msg, target) {
    performRequest(hostname + "/static/templates/alert.html", "GET", "", function(result) {
        var replaced = result.replace("{{type}}", type);
        replaced = replaced.replace("{{msg}}", msg);
        if (typeof target === 'string' || target instanceof String) {
            target = $(target);
        }
        target.hide().empty().append(replaced).fadeIn(400);
    });
}

// A queue for notify alerts to limit number of alerts on screen at a time.
function AlertNotifier(maxAlerts, delay) {
    var notifyQueue = [];
    var alertCount = 0;

    // Show alert if no alerts waiting in queue, else add alert to queue.
    this.queueAlert = function(msg, type) {
        var alert = {msg: msg, type: type};

        if (alertCount < maxAlerts) {
            this.showAlert(alert);
            return;
        }

        notifyQueue.push(alert);
    };

    // Show an alert.
    this.showAlert = function(alert) {
        alertCount++;

        var icon = "glyphicon glyphicon-ok";
        if (alert.type === "warning" || alert.type === "danger") {
            icon = "glyphicon glyphicon-remove"
        }

        var self = this;

        $.notify({
            message: "<strong>" + alert.msg + "</strong>",
            icon: icon
        }, {
            type: alert.type,
            delay: delay,
            newest_on_top: true,
            mouse_over: "pause",
            onClosed: function() {
                self.onAlertClose();
            }
        });
    };

    // Triggered when an alert closes; checks the head of the queue for an alert to show.
    this.onAlertClose = function() {
        if (notifyQueue.length > 0) {
            var poppedAlert = notifyQueue.shift();
            this.showAlert(poppedAlert)
        }
        alertCount--;
    };
}

// Toggle button enabled & spinner visibility.
function setButtonProcessing(element, enabled) {
    if (enabled === true) {
        element.find(".btn-label").css("display", "none");
        element.find(".btn-spinner").css("display", "inline-block");
        element.attr("disabled", true);
        return
    }
    element.find(".btn-label").css("display", "inline-block");
    element.find(".btn-spinner").css("display", "none");
    element.attr("disabled", false);
}
